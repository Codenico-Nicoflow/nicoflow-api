package ai

import (
	"context"
	"errors"
	"fmt"
	"net/http"

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
