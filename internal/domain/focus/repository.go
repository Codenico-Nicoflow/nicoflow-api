package focus

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

type pgRepo struct{ db *pgxpool.Pool }

// NewRepository creates a postgres-backed focus session repository.
func NewRepository(db *pgxpool.Pool) Repository { return &pgRepo{db: db} }

const selectCols = ` id, user_id, task_id, started_at, ended_at, last_seen `

func scan(row pgx.Row, s *Session) error {
	return row.Scan(&s.ID, &s.UserID, &s.TaskID, &s.StartedAt, &s.EndedAt, &s.LastSeen)
}

// OpenAtomic closes any segment the user already has open and starts a new one on
// s.TaskID, in a single transaction. Task ownership is re-checked inside the same
// transaction so a guessed task id can never accrue time against another user's
// task.
func (r *pgRepo) OpenAtomic(ctx context.Context, s Session) (Session, *Session, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Session{}, nil, fmt.Errorf("focus.OpenAtomic begin: %w", err)
	}
	defer func() {
		// Rollback after a successful Commit is a no-op; this only fires on the
		// error paths below.
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			// Best-effort: the transaction is already failing, nothing to salvage.
			_ = rbErr
		}
	}()

	// Serialize this user's opens for the life of the transaction. A row lock on
	// the open segment is NOT enough: when the user has no open segment yet, a
	// SELECT … FOR UPDATE matches zero rows and locks nothing, so concurrent
	// first-opens would race straight into the partial-unique index and one would
	// fail. The advisory lock always exists, so it serializes that case too.
	if _, err := tx.Exec(ctx,
		`SELECT pg_advisory_xact_lock(hashtextextended('focus_open:' || $1, 0))`,
		s.UserID,
	); err != nil {
		return Session{}, nil, fmt.Errorf("focus.OpenAtomic lock user: %w", err)
	}

	// Ownership check inside the tx. A task belonging to someone else is
	// indistinguishable from a missing one — no existence oracle.
	var exists bool
	err = tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM tasks WHERE id = $1 AND user_id = $2)`,
		s.TaskID, s.UserID,
	).Scan(&exists)
	if err != nil {
		return Session{}, nil, fmt.Errorf("focus.OpenAtomic task lookup: %w", err)
	}
	if !exists {
		return Session{}, nil, apperror.New(http.StatusNotFound, apperror.ErrTaskNotFound, "task not found")
	}

	// Read the prior open segment, if any. FOR UPDATE additionally pins the row
	// against a concurrent cascade delete for the rest of the transaction.
	var prev Session
	err = scan(
		tx.QueryRow(ctx,
			`SELECT`+selectCols+`FROM focus_sessions
			 WHERE user_id = $1 AND ended_at IS NULL
			 FOR UPDATE`,
			s.UserID,
		),
		&prev,
	)
	hadOpen := true
	if errors.Is(err, pgx.ErrNoRows) {
		hadOpen = false
	} else if err != nil {
		return Session{}, nil, fmt.Errorf("focus.OpenAtomic lock open: %w", err)
	}

	var closed *Session
	if hadOpen {
		var c Session
		// ended_at = last_seen, never NOW(): the segment is only credited with the
		// time its heartbeats proved.
		if err := scan(
			tx.QueryRow(ctx,
				`UPDATE focus_sessions SET ended_at = last_seen
				 WHERE id = $1 AND ended_at IS NULL
				 RETURNING`+selectCols,
				prev.ID,
			),
			&c,
		); err != nil {
			return Session{}, nil, fmt.Errorf("focus.OpenAtomic close prior: %w", err)
		}
		closed = &c
	}

	var opened Session
	if err := scan(
		tx.QueryRow(ctx,
			`INSERT INTO focus_sessions (id, user_id, task_id, started_at, last_seen)
			 VALUES ($1, $2, $3, NOW(), NOW())
			 RETURNING`+selectCols,
			s.ID, s.UserID, s.TaskID,
		),
		&opened,
	); err != nil {
		return Session{}, nil, fmt.Errorf("focus.OpenAtomic insert: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Session{}, nil, fmt.Errorf("focus.OpenAtomic commit: %w", err)
	}
	return opened, closed, nil
}

func (r *pgRepo) GetOpenByUser(ctx context.Context, userID string) (Session, bool, error) {
	var s Session
	err := scan(
		r.db.QueryRow(ctx,
			`SELECT`+selectCols+`FROM focus_sessions WHERE user_id = $1 AND ended_at IS NULL`,
			userID,
		),
		&s,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("focus.GetOpenByUser: %w", err)
	}
	return s, true, nil
}

func (r *pgRepo) CloseOpenByUser(ctx context.Context, userID string) (Session, bool, error) {
	var s Session
	err := scan(
		r.db.QueryRow(ctx,
			`UPDATE focus_sessions SET ended_at = last_seen
			 WHERE user_id = $1 AND ended_at IS NULL
			 RETURNING`+selectCols,
			userID,
		),
		&s,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// Nothing open — a double-close is a no-op, not a failure.
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("focus.CloseOpenByUser: %w", err)
	}
	return s, true, nil
}

// TouchLastSeen bumps the heartbeat. The id is matched together with user_id, so
// a heartbeat can only ever extend the caller's own segment.
func (r *pgRepo) TouchLastSeen(ctx context.Context, userID, id string) (Session, bool, error) {
	var s Session
	err := scan(
		r.db.QueryRow(ctx,
			`UPDATE focus_sessions SET last_seen = NOW()
			 WHERE id = $1 AND user_id = $2 AND ended_at IS NULL
			 RETURNING`+selectCols,
			id, userID,
		),
		&s,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("focus.TouchLastSeen: %w", err)
	}
	return s, true, nil
}

// ListStaleOpen drives the sweep. Oldest-first with a cap keeps one run bounded;
// anything left over is picked up by the next run.
func (r *pgRepo) ListStaleOpen(ctx context.Context, cutoff time.Time, limit int) ([]Session, error) {
	rows, err := r.db.Query(ctx,
		`SELECT`+selectCols+`FROM focus_sessions
		 WHERE ended_at IS NULL AND last_seen < $1
		 ORDER BY last_seen ASC
		 LIMIT $2`,
		cutoff, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("focus.ListStaleOpen: %w", err)
	}
	defer rows.Close()

	var out []Session
	for rows.Next() {
		var s Session
		if err := scan(rows, &s); err != nil {
			return nil, fmt.Errorf("focus.ListStaleOpen scan: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("focus.ListStaleOpen rows: %w", err)
	}
	return out, nil
}

// CloseByID is the sweep's per-row close. The `ended_at IS NULL` predicate makes
// it idempotent: a segment the owner closed first simply reports ok=false.
func (r *pgRepo) CloseByID(ctx context.Context, id string) (Session, bool, error) {
	var s Session
	err := scan(
		r.db.QueryRow(ctx,
			`UPDATE focus_sessions SET ended_at = last_seen
			 WHERE id = $1 AND ended_at IS NULL
			 RETURNING`+selectCols,
			id,
		),
		&s,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, fmt.Errorf("focus.CloseByID: %w", err)
	}
	return s, true, nil
}

// sumExpr is the one definition of a segment's length. EXTRACT(EPOCH …) yields
// the true elapsed seconds across a DST boundary, which subtracting local dates
// would not.
const sumExpr = `COALESCE(SUM(EXTRACT(EPOCH FROM (ended_at - started_at))), 0)::bigint`

func (r *pgRepo) SumClosedSecondsByTask(ctx context.Context, userID, taskID string) (int64, error) {
	var total int64
	err := r.db.QueryRow(ctx,
		`SELECT `+sumExpr+` FROM focus_sessions
		 WHERE user_id = $1 AND task_id = $2 AND ended_at IS NOT NULL`,
		userID, taskID,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("focus.SumClosedSecondsByTask: %w", err)
	}
	return total, nil
}

// SumClosedSecondsByTaskBatch answers many tasks in one round trip. Tasks with no
// closed segments are absent from the map — callers read a miss as 0.
func (r *pgRepo) SumClosedSecondsByTaskBatch(ctx context.Context, userID string, taskIDs []string) (map[string]int64, error) {
	out := make(map[string]int64, len(taskIDs))
	if len(taskIDs) == 0 {
		return out, nil
	}
	rows, err := r.db.Query(ctx,
		`SELECT task_id, `+sumExpr+` FROM focus_sessions
		 WHERE user_id = $1 AND task_id = ANY($2) AND ended_at IS NOT NULL
		 GROUP BY task_id`,
		userID, taskIDs,
	)
	if err != nil {
		return nil, fmt.Errorf("focus.SumClosedSecondsByTaskBatch: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var taskID string
		var total int64
		if err := rows.Scan(&taskID, &total); err != nil {
			return nil, fmt.Errorf("focus.SumClosedSecondsByTaskBatch scan: %w", err)
		}
		out[taskID] = total
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("focus.SumClosedSecondsByTaskBatch rows: %w", err)
	}
	return out, nil
}
