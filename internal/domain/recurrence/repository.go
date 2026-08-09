package recurrence

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgRepo struct{ db *pgxpool.Pool }

// NewRepository creates a postgres-backed recurrence repository.
func NewRepository(db *pgxpool.Pool) Repository { return &pgRepo{db: db} }

// selectCols is the unaliased projection; cols(alias) qualifies it for a JOIN.
// scheduled_time is a TIME but travels the wire as HH:MM — formatted here so it
// scans straight into a *string, matching the task domain's contract (never the
// seconds Postgres would render).
var selectCols = cols("")

// cols builds the rule projection, optionally qualified by a table alias. A
// builder rather than a string-splitting rewrite: the to_char expression carries
// a comma of its own, which any naive split would tear in half.
func cols(alias string) string {
	if alias != "" {
		alias += "."
	}
	names := []string{
		"id", "user_id", "project_id", "title", "notes", "priority", "energy", "estimated_minutes",
		"freq", "interval", "by_weekday", "by_monthday", "start_date", "end_date",
		"next_occurrence", "paused", "created_at", "updated_at",
	}
	out := make([]string, 0, len(names)+1)
	for _, n := range names {
		out = append(out, alias+n)
	}
	// Appended last so the scan order stays stable as columns are added.
	out = append(out, "to_char("+alias+"scheduled_time, 'HH24:MI')")
	return " " + strings.Join(out, ", ") + " "
}

func scanRule(row pgx.Row, r *Rule) error {
	return row.Scan(ruleFields(r)...)
}

// scanRuleWith scans a rule plus extra trailing columns (the joined timezone on
// the due scan), so the column list stays defined in exactly one place.
func scanRuleWith(row pgx.Row, r *Rule, extra ...any) error {
	return row.Scan(append(ruleFields(r), extra...)...)
}

func ruleFields(r *Rule) []any {
	return []any{
		&r.ID, &r.UserID, &r.ProjectID, &r.Title, &r.Notes, &r.Priority, &r.Energy, &r.EstimatedMinutes,
		&r.Freq, &r.Interval, &r.ByWeekday, &r.ByMonthday, &r.StartDate, &r.EndDate,
		&r.NextOccurrence, &r.Paused, &r.CreatedAt, &r.UpdatedAt, &r.ScheduledTime,
	}
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
			 freq, interval, by_weekday, by_monthday, start_date, end_date, next_occurrence,
			 scheduled_time)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16::time)
		RETURNING`+selectCols,
		rule.ID, rule.UserID, rule.ProjectID, rule.Title, rule.Notes, rule.Priority, rule.Energy, rule.EstimatedMinutes,
		rule.Freq, rule.Interval, rule.ByWeekday, rule.ByMonthday, rule.StartDate, rule.EndDate, rule.NextOccurrence,
		rule.ScheduledTime,
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
			 scheduled_for, scheduled_time, estimated_minutes, display_order,
			 recurrence_rule_id, occurrence_date)
		SELECT $1, $2, $3, $4, $5, 'active', $6, $7, $8::date::text, $11::time, $9,
			COALESCE((SELECT MAX(display_order) + 1 FROM tasks WHERE user_id = $2 AND project_id = $3), 0),
			$10, $8::date
		ON CONFLICT (recurrence_rule_id, occurrence_date) WHERE recurrence_rule_id IS NOT NULL
		DO NOTHING`,
		occ.ID, occ.UserID, occ.ProjectID, occ.Title, occ.Notes, occ.Priority, occ.Energy,
		occ.OccurrenceDate, occ.EstimatedMinutes, occ.RuleID, occ.ScheduledTime,
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
			start_date = $12, end_date = $13, next_occurrence = $14,
			scheduled_time = $15::time, updated_at = NOW()
		WHERE id = $1 AND user_id = $2
		RETURNING`+selectCols,
		rule.ID, rule.UserID, rule.Title, rule.Notes, rule.Priority, rule.Energy, rule.EstimatedMinutes,
		rule.Freq, rule.Interval, rule.ByWeekday, rule.ByMonthday,
		rule.StartDate, rule.EndDate, rule.NextOccurrence, rule.ScheduledTime,
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
			scheduled_time = $9::time,
			updated_at = NOW()
		WHERE recurrence_rule_id = $1 AND user_id = $2 AND status NOT IN ('done', 'cancelled')`,
		rule.ID, rule.UserID, rule.Title, rule.Notes, rule.Priority, rule.Energy, rule.EstimatedMinutes,
		rule.NextOccurrence, rule.ScheduledTime,
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

// ListDue returns rules whose cursor has come due, with the owner's timezone so
// the caller can decide "today" locally. Bounded by `next_occurrence <= today+1`
// in UTC terms: no zone is more than a day from UTC, so this narrows the scan to
// the index while leaving the authoritative local comparison to the caller.
// System-scoped — the only non-user-scoped path, reachable solely from the
// internal cron. Paused and exhausted rules never appear.
func (r *pgRepo) ListDue(ctx context.Context) ([]DueRule, error) {
	rows, err := r.db.Query(ctx, `
		SELECT`+cols("r")+`, COALESCE(u.timezone, 'UTC')
		FROM recurrence_rules r
		JOIN users u ON u.id = r.user_id
		WHERE r.paused = FALSE
		  AND r.next_occurrence IS NOT NULL
		  AND r.next_occurrence <= (NOW() AT TIME ZONE 'UTC')::date + 1
		  AND u.deleted_at IS NULL
		ORDER BY r.next_occurrence ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("recurrence.ListDue: %w", err)
	}
	defer rows.Close()

	var out []DueRule
	for rows.Next() {
		var d DueRule
		if err := scanRuleWith(rows, &d.Rule, &d.Timezone); err != nil {
			return nil, fmt.Errorf("recurrence.ListDue scan: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("recurrence.ListDue rows: %w", err)
	}
	return out, nil
}

func (r *pgRepo) GetForMaterialize(ctx context.Context, userID, ruleID string) (Rule, error) {
	return r.GetByID(ctx, userID, ruleID)
}

// Materialize is the whole state transition in one transaction: reap the prior
// un-done instance to `missed`, insert the new occurrence, advance the cursor.
//
// The plan check sits inside the INSERT so a concurrent create can't slip the
// project over its limit between a read and a write. When it trips, the cursor is
// deliberately left untouched — advancing past an occurrence the user never saw
// is data loss, so the sweep retries on the next tick.
func (r *pgRepo) Materialize(ctx context.Context, rule Rule, occ Occurrence, limit int) (MaterializeResult, error) {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return MaterializeResult{}, fmt.Errorf("recurrence.Materialize begin: %w", err)
	}
	defer func() {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			_ = rbErr
		}
	}()

	// The horizon is one live instance per rule. If an un-done one already exists
	// for a *different* date, the window hasn't closed yet — leave it alone.
	// occurrence_status IS NULL excludes rows already reaped as missed (those
	// also carry status='cancelled', but checking occurrence_status directly
	// keeps this query correct even if a future caller cancels a task some
	// other way).
	var liveDate *time.Time
	err = tx.QueryRow(ctx, `
		SELECT occurrence_date FROM tasks
		WHERE recurrence_rule_id = $1 AND user_id = $2
		  AND status NOT IN ('done', 'cancelled')
		  AND occurrence_status IS NULL
		ORDER BY occurrence_date DESC LIMIT 1`,
		rule.ID, rule.UserID,
	).Scan(&liveDate)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return MaterializeResult{}, fmt.Errorf("recurrence.Materialize live lookup: %w", err)
	}
	if liveDate != nil && !liveDate.Before(occ.OccurrenceDate) {
		return MaterializeResult{SkippedExisting: true}, nil
	}

	// Guarded insert: the row appears only while the project is under its
	// active-task limit. 0 rows ⇒ the limit would be exceeded.
	tag, err := tx.Exec(ctx, `
		INSERT INTO tasks
			(id, user_id, project_id, title, notes, status, priority, energy,
			 scheduled_for, scheduled_time, estimated_minutes, display_order,
			 recurrence_rule_id, occurrence_date)
		SELECT $1, $2, $3, $4, $5, 'active', $6, $7, $8::date::text, $12::time, $9,
			COALESCE((SELECT MAX(display_order) + 1 FROM tasks WHERE user_id = $2 AND project_id = $3), 0),
			$10, $8::date
		WHERE (SELECT COUNT(*) FROM tasks
		       WHERE user_id = $2 AND project_id = $3 AND status = 'active') < $11
		ON CONFLICT (recurrence_rule_id, occurrence_date) WHERE recurrence_rule_id IS NOT NULL
		DO NOTHING`,
		occ.ID, occ.UserID, occ.ProjectID, occ.Title, occ.Notes, occ.Priority, occ.Energy,
		occ.OccurrenceDate, occ.EstimatedMinutes, occ.RuleID, limit, occ.ScheduledTime,
	)
	if err != nil {
		return MaterializeResult{}, fmt.Errorf("recurrence.Materialize insert: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return classifyNoInsert(ctx, tx, rule.ID, occ.OccurrenceDate)
	}

	// Reap the window that just closed: the prior un-done instance is marked
	// occurrence_status='missed' (the streak calculator's signal that it lapsed
	// rather than was cancelled) and its status flips to 'cancelled' so it drops
	// out of the plan-limit count, Focus, and Time Spread exactly like any other
	// cancelled task — no special-casing needed in the task domain's queries.
	reap, err := tx.Exec(ctx, `
		UPDATE tasks SET status = 'cancelled', occurrence_status = 'missed', updated_at = NOW()
		WHERE recurrence_rule_id = $1 AND user_id = $2
		  AND occurrence_date < $3
		  AND status NOT IN ('done', 'cancelled')
		  AND occurrence_status IS NULL`,
		rule.ID, rule.UserID, occ.OccurrenceDate,
	)
	if err != nil {
		return MaterializeResult{}, fmt.Errorf("recurrence.Materialize reap: %w", err)
	}

	if _, err := tx.Exec(ctx,
		`UPDATE recurrence_rules SET next_occurrence = $3, updated_at = NOW()
		 WHERE id = $1 AND user_id = $2`,
		rule.ID, rule.UserID, rule.NextOccurrence,
	); err != nil {
		return MaterializeResult{}, fmt.Errorf("recurrence.Materialize advance: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return MaterializeResult{}, fmt.Errorf("recurrence.Materialize commit: %w", err)
	}
	return MaterializeResult{Created: &occ, Reaped: reap.RowsAffected() > 0}, nil
}

// classifyNoInsert explains a zero-row insert: either the plan guard tripped, or
// the unique index caught a concurrent write. A row that now exists means we lost
// a benign race and the occurrence is present either way — success, not a stall.
func classifyNoInsert(ctx context.Context, tx pgx.Tx, ruleID string, occDate time.Time) (MaterializeResult, error) {
	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM tasks WHERE recurrence_rule_id = $1 AND occurrence_date = $2)`,
		ruleID, occDate,
	).Scan(&exists); err != nil {
		return MaterializeResult{}, fmt.Errorf("recurrence.Materialize conflict probe: %w", err)
	}
	if !exists {
		return MaterializeResult{SkippedPlanLimit: true}, nil
	}
	if err := tx.Commit(ctx); err != nil {
		return MaterializeResult{}, fmt.Errorf("recurrence.Materialize commit: %w", err)
	}
	return MaterializeResult{SkippedExisting: true}, nil
}

func (r *pgRepo) CountOccurrencesByStatus(ctx context.Context, userID, ruleID string) (map[string]int, error) {
	rows, err := r.db.Query(ctx,
		`SELECT COALESCE(occurrence_status, status), COUNT(*) FROM tasks
		 WHERE recurrence_rule_id = $1 AND user_id = $2
		 GROUP BY COALESCE(occurrence_status, status)`,
		ruleID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("recurrence.CountOccurrencesByStatus: %w", err)
	}
	defer rows.Close()

	out := map[string]int{}
	for rows.Next() {
		var status string
		var n int
		if err := rows.Scan(&status, &n); err != nil {
			return nil, fmt.Errorf("recurrence.CountOccurrencesByStatus scan: %w", err)
		}
		out[status] = n
	}
	return out, rows.Err()
}

// ListOccurrenceStatuses returns a rule's occurrence statuses newest-first — the
// order the streak walk needs.
func (r *pgRepo) ListOccurrenceStatuses(ctx context.Context, userID, ruleID string) ([]string, error) {
	rows, err := r.db.Query(ctx,
		`SELECT COALESCE(occurrence_status, status) FROM tasks
		 WHERE recurrence_rule_id = $1 AND user_id = $2
		 ORDER BY occurrence_date DESC`,
		ruleID, userID,
	)
	if err != nil {
		return nil, fmt.Errorf("recurrence.ListOccurrenceStatuses: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, fmt.Errorf("recurrence.ListOccurrenceStatuses scan: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
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
