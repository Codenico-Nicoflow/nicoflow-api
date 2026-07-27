package ai

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// Repository defines the AI assistant data access interface.
type Repository interface {
	CreateSession(ctx context.Context, s Session) (Session, error)
	ListSessions(ctx context.Context, userID string) ([]Session, error)
	GetSession(ctx context.Context, userID, id string) (*Session, error)
	ListMessages(ctx context.Context, sessionID string) ([]SessionMessage, error)
	DeleteSession(ctx context.Context, userID, id string) error
	// UsageSum returns SUM(request_count) across all the user's rows (Free lifetime).
	UsageSum(ctx context.Context, userID string) (int, error)
	// UsageForMonth returns request_count for the given YYYY-MM-01 month (Pro).
	UsageForMonth(ctx context.Context, userID, month string) (int, error)
	// ReserveMonthly atomically takes one request from the user's month row,
	// guarded so request_count never exceeds limit. Returns the usage row id;
	// exhausted quota → 429 AI_LIMIT_REACHED.
	ReserveMonthly(ctx context.Context, userID, month string, limit int) (string, error)
	// ReserveLifetime atomically takes one request guarded by the lifetime
	// SUM(request_count) across all the user's rows. Same return contract.
	ReserveLifetime(ctx context.Context, userID, month string, limit int) (string, error)
	// RefundUsage gives back one reserved request on the given usage row.
	RefundUsage(ctx context.Context, usageID string) error
	// AppendUserMessage inserts a user turn. When it is the session's first
	// message, it also sets the session title (word-boundary-truncated) in the
	// same transaction. All within one tx so title and message are atomic.
	AppendUserMessage(ctx context.Context, sessionID, msgID, content, title string) error
	// AppendAssistantMessage inserts the assistant turn and bumps the session's
	// updated_at, so a completed (or aborted-with-partial) stream is one write.
	AppendAssistantMessage(ctx context.Context, sessionID, msgID, content string) error
	// HistoryFor returns the session's messages, newest-first, capped at limit
	// rows — the budget builder trims further by character count.
	HistoryFor(ctx context.Context, sessionID string, limit int) ([]SessionMessage, error)
	// PromptContext returns the volatile system-prompt inputs for a user: their
	// language and open-task COUNT(*). One round-trip, row-isolated by user_id.
	PromptContext(ctx context.Context, userID string) (PromptContext, error)
}

type pgRepo struct{ db *pgxpool.Pool }

// NewRepository creates a new postgres-backed AI repository.
func NewRepository(db *pgxpool.Pool) Repository { return &pgRepo{db: db} }

func (r *pgRepo) CreateSession(ctx context.Context, s Session) (Session, error) {
	err := r.db.QueryRow(ctx, `
		INSERT INTO ai_sessions (id, user_id, title, created_at, updated_at)
		VALUES ($1, $2, COALESCE(NULLIF($3, ''), 'New Conversation'), NOW(), NOW())
		RETURNING id, user_id, title, created_at, updated_at`,
		s.ID, s.UserID, s.Title,
	).Scan(&s.ID, &s.UserID, &s.Title, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return Session{}, fmt.Errorf("ai.CreateSession: %w", err)
	}
	return s, nil
}

func (r *pgRepo) ListSessions(ctx context.Context, userID string) ([]Session, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, user_id, title, created_at, updated_at
		FROM ai_sessions
		WHERE user_id = $1
		ORDER BY updated_at DESC, id DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("ai.ListSessions query: %w", err)
	}
	defer rows.Close()

	sessions := []Session{}
	for rows.Next() {
		var s Session
		if err := rows.Scan(&s.ID, &s.UserID, &s.Title, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("ai.ListSessions scan: %w", err)
		}
		sessions = append(sessions, s)
	}
	return sessions, rows.Err()
}

func (r *pgRepo) GetSession(ctx context.Context, userID, id string) (*Session, error) {
	var s Session
	err := r.db.QueryRow(ctx, `
		SELECT id, user_id, title, created_at, updated_at
		FROM ai_sessions
		WHERE id = $1 AND user_id = $2`,
		id, userID,
	).Scan(&s.ID, &s.UserID, &s.Title, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "session not found")
		}
		return nil, fmt.Errorf("ai.GetSession: %w", err)
	}
	return &s, nil
}

func (r *pgRepo) ListMessages(ctx context.Context, sessionID string) ([]SessionMessage, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, session_id, role, content, created_at
		FROM ai_messages
		WHERE session_id = $1
		ORDER BY created_at ASC, id ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("ai.ListMessages query: %w", err)
	}
	defer rows.Close()

	msgs := []SessionMessage{}
	for rows.Next() {
		var m SessionMessage
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("ai.ListMessages scan: %w", err)
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (r *pgRepo) DeleteSession(ctx context.Context, userID, id string) error {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM ai_sessions WHERE id = $1 AND user_id = $2`,
		id, userID,
	)
	if err != nil {
		return fmt.Errorf("ai.DeleteSession: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "session not found")
	}
	return nil
}

func (r *pgRepo) UsageSum(ctx context.Context, userID string) (int, error) {
	var used int
	err := r.db.QueryRow(ctx,
		`SELECT COALESCE(SUM(request_count), 0) FROM ai_usage_monthly WHERE user_id = $1`,
		userID,
	).Scan(&used)
	if err != nil {
		return 0, fmt.Errorf("ai.UsageSum: %w", err)
	}
	return used, nil
}

// errQuotaExhausted is the shared over-quota error. The guarded statement
// returning 0 rows IS the check — never a pre-flight COUNT (TOCTOU race).
func errQuotaExhausted() error {
	return apperror.New(http.StatusTooManyRequests, apperror.ErrAILimitReached, "AI request limit reached")
}

func (r *pgRepo) ReserveMonthly(ctx context.Context, userID, month string, limit int) (string, error) {
	// The conflict path locks the month row, so the guard is re-evaluated on
	// the current value — concurrent bursts serialize and cannot exceed limit.
	var id string
	err := r.db.QueryRow(ctx, `
		INSERT INTO ai_usage_monthly (id, user_id, month, request_count)
		VALUES ($1, $2, $3::date, 1)
		ON CONFLICT (user_id, month) DO UPDATE
		  SET request_count = ai_usage_monthly.request_count + 1
		  WHERE ai_usage_monthly.request_count < $4
		RETURNING id`,
		uuid.New().String(), userID, month, limit,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errQuotaExhausted()
	}
	if err != nil {
		return "", fmt.Errorf("ai.ReserveMonthly: %w", err)
	}
	return id, nil
}

func (r *pgRepo) ReserveLifetime(ctx context.Context, userID, month string, limit int) (string, error) {
	// The lifetime guard is a SUM over rows the statement's row lock does not
	// cover (other months, or no rows at all for a new user), so a per-user
	// advisory lock serializes concurrent reserves before the guarded insert.
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("ai.ReserveLifetime begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('ai_usage:' || $1, 0))`,
		userID,
	); err != nil {
		return "", fmt.Errorf("ai.ReserveLifetime lock: %w", err)
	}

	var id string
	err = tx.QueryRow(ctx, `
		INSERT INTO ai_usage_monthly (id, user_id, month, request_count)
		SELECT $1, $2, $3::date, 1
		WHERE (SELECT COALESCE(SUM(request_count), 0) FROM ai_usage_monthly WHERE user_id = $2) < $4
		ON CONFLICT (user_id, month) DO UPDATE
		  SET request_count = ai_usage_monthly.request_count + 1
		  WHERE (SELECT COALESCE(SUM(request_count), 0) FROM ai_usage_monthly WHERE user_id = $2) < $4
		RETURNING id`,
		uuid.New().String(), userID, month, limit,
	).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errQuotaExhausted()
	}
	if err != nil {
		return "", fmt.Errorf("ai.ReserveLifetime: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return "", fmt.Errorf("ai.ReserveLifetime commit: %w", err)
	}
	return id, nil
}

func (r *pgRepo) RefundUsage(ctx context.Context, usageID string) error {
	_, err := r.db.Exec(ctx,
		`UPDATE ai_usage_monthly SET request_count = request_count - 1
		 WHERE id = $1 AND request_count > 0`,
		usageID,
	)
	if err != nil {
		return fmt.Errorf("ai.RefundUsage: %w", err)
	}
	return nil
}

func (r *pgRepo) AppendUserMessage(ctx context.Context, sessionID, msgID, content, title string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ai.AppendUserMessage begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var count int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM ai_messages WHERE session_id = $1`, sessionID,
	).Scan(&count); err != nil {
		return fmt.Errorf("ai.AppendUserMessage count: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO ai_messages (id, session_id, role, content, created_at)
		 VALUES ($1, $2, 'user', $3, NOW())`,
		msgID, sessionID, content,
	); err != nil {
		return fmt.Errorf("ai.AppendUserMessage insert: %w", err)
	}

	// First turn seeds the session title from the message.
	if count == 0 && title != "" {
		if _, err := tx.Exec(ctx,
			`UPDATE ai_sessions SET title = $2, updated_at = NOW() WHERE id = $1`,
			sessionID, title,
		); err != nil {
			return fmt.Errorf("ai.AppendUserMessage title: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("ai.AppendUserMessage commit: %w", err)
	}
	return nil
}

func (r *pgRepo) AppendAssistantMessage(ctx context.Context, sessionID, msgID, content string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("ai.AppendAssistantMessage begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx,
		`INSERT INTO ai_messages (id, session_id, role, content, created_at)
		 VALUES ($1, $2, 'assistant', $3, NOW())`,
		msgID, sessionID, content,
	); err != nil {
		return fmt.Errorf("ai.AppendAssistantMessage insert: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE ai_sessions SET updated_at = NOW() WHERE id = $1`, sessionID,
	); err != nil {
		return fmt.Errorf("ai.AppendAssistantMessage touch: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("ai.AppendAssistantMessage commit: %w", err)
	}
	return nil
}

func (r *pgRepo) HistoryFor(ctx context.Context, sessionID string, limit int) ([]SessionMessage, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, session_id, role, content, created_at
		FROM ai_messages
		WHERE session_id = $1
		ORDER BY created_at DESC, id DESC
		LIMIT $2`,
		sessionID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("ai.HistoryFor query: %w", err)
	}
	defer rows.Close()

	msgs := []SessionMessage{}
	for rows.Next() {
		var m SessionMessage
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &m.CreatedAt); err != nil {
			return nil, fmt.Errorf("ai.HistoryFor scan: %w", err)
		}
		msgs = append(msgs, m)
	}
	return msgs, rows.Err()
}

func (r *pgRepo) PromptContext(ctx context.Context, userID string) (PromptContext, error) {
	var pc PromptContext
	err := r.db.QueryRow(ctx, `
		SELECT
			COALESCE((SELECT language FROM users WHERE id = $1 AND deleted_at IS NULL), 'en'),
			(SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND status NOT IN ('done', 'cancelled'))`,
		userID,
	).Scan(&pc.Language, &pc.OpenTasks)
	if err != nil {
		return PromptContext{}, fmt.Errorf("ai.PromptContext: %w", err)
	}
	return pc, nil
}

func (r *pgRepo) UsageForMonth(ctx context.Context, userID, month string) (int, error) {
	var used int
	err := r.db.QueryRow(ctx,
		`SELECT COALESCE(request_count, 0) FROM ai_usage_monthly WHERE user_id = $1 AND month = $2`,
		userID, month,
	).Scan(&used)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("ai.UsageForMonth: %w", err)
	}
	return used, nil
}
