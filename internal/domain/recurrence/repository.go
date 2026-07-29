package recurrence

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgRepo struct{ db *pgxpool.Pool }

// NewRepository creates a postgres-backed recurrence repository.
func NewRepository(db *pgxpool.Pool) Repository { return &pgRepo{db: db} }

const selectCols = ` id, user_id, project_id, title, notes, priority, energy, estimated_minutes,
	freq, interval, by_weekday, by_monthday, start_date, end_date,
	next_occurrence, paused, created_at, updated_at `

func scanRule(row pgx.Row, r *Rule) error {
	return row.Scan(
		&r.ID, &r.UserID, &r.ProjectID, &r.Title, &r.Notes, &r.Priority, &r.Energy, &r.EstimatedMinutes,
		&r.Freq, &r.Interval, &r.ByWeekday, &r.ByMonthday, &r.StartDate, &r.EndDate,
		&r.NextOccurrence, &r.Paused, &r.CreatedAt, &r.UpdatedAt,
	)
}

// CreateWithOccurrence writes the rule and its first task in one transaction, so
// a rule can never exist without the instance the user expects to see.
func (r *pgRepo) CreateWithOccurrence(ctx context.Context, rule Rule, occ Occurrence) (Rule, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Rule{}, fmt.Errorf("recurrence.CreateWithOccurrence begin: %w", err)
	}
	defer func() {
		// Rollback after a successful Commit is a no-op; this only fires on the
		// error paths below.
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			// Best-effort: the transaction is already failing, nothing to salvage.
			_ = rbErr
		}
	}()

	var out Rule
	err = scanRule(tx.QueryRow(ctx, `
		INSERT INTO recurrence_rules
			(id, user_id, project_id, title, notes, priority, energy, estimated_minutes,
			 freq, interval, by_weekday, by_monthday, start_date, end_date, next_occurrence)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING`+selectCols,
		rule.ID, rule.UserID, rule.ProjectID, rule.Title, rule.Notes, rule.Priority, rule.Energy, rule.EstimatedMinutes,
		rule.Freq, rule.Interval, rule.ByWeekday, rule.ByMonthday, rule.StartDate, rule.EndDate, rule.NextOccurrence,
	), &out)
	if err != nil {
		return Rule{}, fmt.Errorf("recurrence.CreateWithOccurrence insert rule: %w", err)
	}

	if err := insertOccurrence(ctx, tx, occ); err != nil {
		return Rule{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return Rule{}, fmt.Errorf("recurrence.CreateWithOccurrence commit: %w", err)
	}
	return out, nil
}

// insertOccurrence materializes one task from the rule template. display_order is
// resolved inside the transaction so concurrent creates in the same project can't
// both claim the same slot. The partial unique index on
// (recurrence_rule_id, occurrence_date) makes a duplicate a no-op rather than an
// error — that is the idempotency guarantee the sweep relies on.
func insertOccurrence(ctx context.Context, tx pgx.Tx, occ Occurrence) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO tasks
			(id, user_id, project_id, title, notes, status, priority, energy,
			 scheduled_for, estimated_minutes, display_order, recurrence_rule_id, occurrence_date)
		SELECT $1, $2, $3, $4, $5, 'active', $6, $7, $8::date::text, $9,
			COALESCE((SELECT MAX(display_order) + 1 FROM tasks WHERE user_id = $2 AND project_id = $3), 0),
			$10, $8::date
		ON CONFLICT (recurrence_rule_id, occurrence_date) WHERE recurrence_rule_id IS NOT NULL
		DO NOTHING`,
		occ.ID, occ.UserID, occ.ProjectID, occ.Title, occ.Notes, occ.Priority, occ.Energy,
		occ.OccurrenceDate, occ.EstimatedMinutes, occ.RuleID,
	)
	if err != nil {
		return fmt.Errorf("recurrence.insertOccurrence: %w", err)
	}
	return nil
}

func (r *pgRepo) List(ctx context.Context, userID string, projectID *string) ([]Rule, error) {
	// A single statement with a NULL-tolerant predicate keeps the SQL static —
	// no concatenation, so the optional filter can't become an injection seam.
	rows, err := r.db.Query(ctx, `
		SELECT`+selectCols+`FROM recurrence_rules
		WHERE user_id = $1 AND ($2::text IS NULL OR project_id = $2)
		ORDER BY created_at DESC`,
		userID, projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("recurrence.List: %w", err)
	}
	defer rows.Close()

	var out []Rule
	for rows.Next() {
		var rule Rule
		if err := scanRule(rows, &rule); err != nil {
			return nil, fmt.Errorf("recurrence.List scan: %w", err)
		}
		out = append(out, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recurrence.List rows: %w", err)
	}
	return out, nil
}

// GetByID is user-scoped: another user's rule is indistinguishable from a missing
// one, so the 404 leaks no existence information.
func (r *pgRepo) GetByID(ctx context.Context, userID, id string) (Rule, error) {
	var out Rule
	err := scanRule(r.db.QueryRow(ctx,
		`SELECT`+selectCols+`FROM recurrence_rules WHERE id = $1 AND user_id = $2`,
		id, userID,
	), &out)
	if errors.Is(err, pgx.ErrNoRows) {
		return Rule{}, errRuleNotFound()
	}
	if err != nil {
		return Rule{}, fmt.Errorf("recurrence.GetByID: %w", err)
	}
	return out, nil
}

// Update writes the new template and re-stamps the live instance in one
// transaction. "Live" is the single un-done, un-cancelled occurrence; past rows
// are left alone so history is never rewritten.
func (r *pgRepo) Update(ctx context.Context, rule Rule) (Rule, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return Rule{}, fmt.Errorf("recurrence.Update begin: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			_ = rbErr
		}
	}()

	var out Rule
	err = scanRule(tx.QueryRow(ctx, `
		UPDATE recurrence_rules SET
			title = $3, notes = $4, priority = $5, energy = $6, estimated_minutes = $7,
			freq = $8, interval = $9, by_weekday = $10, by_monthday = $11,
			start_date = $12, end_date = $13, next_occurrence = $14, updated_at = NOW()
		WHERE id = $1 AND user_id = $2
		RETURNING`+selectCols,
		rule.ID, rule.UserID, rule.Title, rule.Notes, rule.Priority, rule.Energy, rule.EstimatedMinutes,
		rule.Freq, rule.Interval, rule.ByWeekday, rule.ByMonthday,
		rule.StartDate, rule.EndDate, rule.NextOccurrence,
	), &out)
	if errors.Is(err, pgx.ErrNoRows) {
		return Rule{}, errRuleNotFound()
	}
	if err != nil {
		return Rule{}, fmt.Errorf("recurrence.Update: %w", err)
	}

	// Re-stamping is unconditional — no per-field dirty tracking — so a manual
	// rename of the live instance can be overwritten. Accepted (NIC-1772).
	if _, err := tx.Exec(ctx, `
		UPDATE tasks SET
			title = $3, notes = $4, priority = $5, energy = $6, estimated_minutes = $7,
			scheduled_for = COALESCE($8::date::text, scheduled_for),
			occurrence_date = COALESCE($8::date, occurrence_date),
			updated_at = NOW()
		WHERE recurrence_rule_id = $1 AND user_id = $2 AND status NOT IN ('done', 'cancelled')`,
		rule.ID, rule.UserID, rule.Title, rule.Notes, rule.Priority, rule.Energy, rule.EstimatedMinutes,
		rule.NextOccurrence,
	); err != nil {
		return Rule{}, fmt.Errorf("recurrence.Update restamp: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Rule{}, fmt.Errorf("recurrence.Update commit: %w", err)
	}
	return out, nil
}

func (r *pgRepo) SetPaused(ctx context.Context, userID, id string, paused bool) (Rule, error) {
	var out Rule
	err := scanRule(r.db.QueryRow(ctx, `
		UPDATE recurrence_rules SET paused = $3, updated_at = NOW()
		WHERE id = $1 AND user_id = $2
		RETURNING`+selectCols,
		id, userID, paused,
	), &out)
	if errors.Is(err, pgx.ErrNoRows) {
		return Rule{}, errRuleNotFound()
	}
	if err != nil {
		return Rule{}, fmt.Errorf("recurrence.SetPaused: %w", err)
	}
	return out, nil
}

// Delete removes the pending occurrence, then the rule. Past occurrences survive
// with a NULL recurrence_rule_id via the FK's ON DELETE SET NULL.
func (r *pgRepo) Delete(ctx context.Context, userID, id string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("recurrence.Delete begin: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			_ = rbErr
		}
	}()

	if _, err := tx.Exec(ctx,
		`DELETE FROM tasks
		 WHERE recurrence_rule_id = $1 AND user_id = $2 AND status NOT IN ('done', 'cancelled')`,
		id, userID,
	); err != nil {
		return fmt.Errorf("recurrence.Delete pending occurrence: %w", err)
	}

	tag, err := tx.Exec(ctx, `DELETE FROM recurrence_rules WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return fmt.Errorf("recurrence.Delete: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errRuleNotFound()
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("recurrence.Delete commit: %w", err)
	}
	return nil
}

func (r *pgRepo) CountByUser(ctx context.Context, userID string) (int, error) {
	var count int
	if err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM recurrence_rules WHERE user_id = $1`, userID,
	).Scan(&count); err != nil {
		return 0, fmt.Errorf("recurrence.CountByUser: %w", err)
	}
	return count, nil
}

func (r *pgRepo) ProjectOwned(ctx context.Context, userID, projectID string) (bool, error) {
	var exists bool
	if err := r.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM projects WHERE id = $1 AND user_id = $2)`,
		projectID, userID,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("recurrence.ProjectOwned: %w", err)
	}
	return exists, nil
}
