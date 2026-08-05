package habit

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

// NewRepository creates a postgres-backed habit repository.
func NewRepository(db *pgxpool.Pool) Repository { return &pgRepo{db: db} }

const habitCols = ` id, user_id, name, subject, color, polarity, target_value, unit,
	schedule_kind, by_weekday, times_per_week, day_cutoff_hour, schedule_changed_at,
	archived_at, created_at, updated_at `

func scanHabit(row pgx.Row, h *Habit) error {
	return row.Scan(&h.ID, &h.UserID, &h.Name, &h.Subject, &h.Color, &h.Polarity,
		&h.TargetValue, &h.Unit, &h.ScheduleKind, &h.ByWeekday, &h.TimesPerWeek,
		&h.DayCutoffHour, &h.ScheduleChangedAt, &h.ArchivedAt, &h.CreatedAt, &h.UpdatedAt)
}

func notFound() error {
	return apperror.New(http.StatusNotFound, apperror.ErrHabitNotFound, "habit not found")
}

// List orders newest-first so a freshly created habit is visible without the
// client sorting. includeArchived is a parameter rather than two methods because
// the only difference is one predicate.
func (r *pgRepo) List(ctx context.Context, userID string, includeArchived bool) ([]Habit, error) {
	q := `SELECT` + habitCols + `FROM habits WHERE user_id = $1`
	if !includeArchived {
		q += ` AND archived_at IS NULL`
	}
	q += ` ORDER BY created_at DESC`

	rows, err := r.db.Query(ctx, q, userID)
	if err != nil {
		return nil, fmt.Errorf("habit.List: %w", err)
	}
	defer rows.Close()

	var out []Habit
	for rows.Next() {
		var h Habit
		if err := scanHabit(rows, &h); err != nil {
			return nil, fmt.Errorf("habit.List scan: %w", err)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("habit.List rows: %w", err)
	}
	return out, nil
}

func (r *pgRepo) GetByID(ctx context.Context, userID, id string) (Habit, error) {
	var h Habit
	err := scanHabit(
		r.db.QueryRow(ctx,
			`SELECT`+habitCols+`FROM habits WHERE id = $1 AND user_id = $2`,
			id, userID,
		),
		&h,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		// Not-owned is reported exactly like missing — never an existence leak.
		return Habit{}, notFound()
	}
	if err != nil {
		return Habit{}, fmt.Errorf("habit.GetByID: %w", err)
	}
	return h, nil
}

// Create lets the database stamp created_at/updated_at from the column defaults
// so it stays the single source of truth for them.
func (r *pgRepo) Create(ctx context.Context, h Habit) (Habit, error) {
	var out Habit
	err := scanHabit(
		r.db.QueryRow(ctx,
			`INSERT INTO habits (id, user_id, name, subject, color, polarity,
				target_value, unit, schedule_kind, by_weekday, times_per_week)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			 RETURNING`+habitCols,
			h.ID, h.UserID, h.Name, h.Subject, h.Color, h.Polarity,
			h.TargetValue, h.Unit, h.ScheduleKind, h.ByWeekday, h.TimesPerWeek,
		),
		&out,
	)
	if err != nil {
		return Habit{}, fmt.Errorf("habit.Create: %w", err)
	}
	return out, nil
}

// Update writes the merged row the service produced. schedule_changed_at and
// archived_at are conditional: COALESCE would be wrong for archived_at, since
// restoring a habit must be able to write NULL.
func (r *pgRepo) Update(ctx context.Context, p UpdateParams) (Habit, bool, error) {
	var out Habit
	err := scanHabit(
		r.db.QueryRow(ctx,
			`UPDATE habits
			    SET name = $3, subject = $4, color = $5,
			        target_value = $6, unit = $7,
			        schedule_kind = $8, by_weekday = $9, times_per_week = $10,
			        schedule_changed_at = COALESCE($11::date, schedule_changed_at),
			        archived_at = CASE
			            WHEN $12::boolean IS NULL THEN archived_at
			            WHEN $12::boolean         THEN COALESCE(archived_at, NOW())
			            ELSE NULL
			        END,
			        updated_at = NOW()
			  WHERE id = $1 AND user_id = $2
			  RETURNING`+habitCols,
			p.ID, p.UserID, p.Name, p.Subject, p.Color,
			p.TargetValue, p.Unit, p.ScheduleKind, p.ByWeekday, p.TimesPerWeek,
			p.ScheduleChangedAt, p.Archived,
		),
		&out,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return Habit{}, false, nil
	}
	if err != nil {
		return Habit{}, false, fmt.Errorf("habit.Update: %w", err)
	}
	return out, true, nil
}

// Archive is idempotent on an already-archived habit: the WHERE matches the row
// either way and COALESCE keeps the original archival instant, so a repeated
// DELETE does not silently reset when the habit was retired.
func (r *pgRepo) Archive(ctx context.Context, userID, id string) (bool, error) {
	tag, err := r.db.Exec(ctx,
		`UPDATE habits
		    SET archived_at = COALESCE(archived_at, NOW()), updated_at = NOW()
		  WHERE id = $1 AND user_id = $2`,
		id, userID,
	)
	if err != nil {
		return false, fmt.Errorf("habit.Archive: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// UpsertCheckIn writes one dated entry. The ON CONFLICT clause rides the unique
// index on (habit_id, check_in_date), which is what makes a repeated check-in
// idempotent at the database rather than through a read-then-write that two
// concurrent taps could both lose.
//
// target_at_checkin and satisfied are re-stamped on conflict: they describe the
// judgement made at *this* write, and the caller has already computed them from
// the habit's current target.
func (r *pgRepo) UpsertCheckIn(ctx context.Context, c CheckIn) (CheckIn, error) {
	var out CheckIn
	err := r.db.QueryRow(ctx,
		`INSERT INTO habit_check_ins
			(id, habit_id, user_id, check_in_date, value, target_at_checkin, satisfied)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (habit_id, check_in_date) DO UPDATE
		    SET value = EXCLUDED.value,
		        target_at_checkin = EXCLUDED.target_at_checkin,
		        satisfied = EXCLUDED.satisfied,
		        updated_at = NOW()
		 RETURNING id, habit_id, user_id, check_in_date, value, target_at_checkin,
		           satisfied, created_at, updated_at`,
		c.ID, c.HabitID, c.UserID, c.Date, c.Value, c.TargetAt, c.Satisfied,
	).Scan(&out.ID, &out.HabitID, &out.UserID, &out.Date, &out.Value, &out.TargetAt,
		&out.Satisfied, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return CheckIn{}, fmt.Errorf("habit.UpsertCheckIn: %w", err)
	}
	return out, nil
}

func (r *pgRepo) DeleteCheckIn(ctx context.Context, userID, habitID string, date time.Time) (bool, error) {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM habit_check_ins
		  WHERE habit_id = $1 AND user_id = $2 AND check_in_date = $3`,
		habitID, userID, date,
	)
	if err != nil {
		return false, fmt.Errorf("habit.DeleteCheckIn: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// ListCheckIns loads history for a set of habits in one query. Batching matters:
// a list read derives every habit's streak, and one query per habit is the shape
// that makes derived-on-read expensive enough to regret.
//
// Returns an empty map for an empty habit set rather than issuing a query with
// an empty ANY(), which matches no rows but still costs a round trip.
func (r *pgRepo) ListCheckIns(ctx context.Context, userID string, habitIDs []string, since time.Time) (map[string][]CheckIn, error) {
	out := make(map[string][]CheckIn, len(habitIDs))
	if len(habitIDs) == 0 {
		return out, nil
	}

	rows, err := r.db.Query(ctx,
		`SELECT id, habit_id, user_id, check_in_date, value, target_at_checkin,
		        satisfied, created_at, updated_at
		   FROM habit_check_ins
		  WHERE user_id = $1 AND habit_id = ANY($2) AND check_in_date >= $3
		  ORDER BY check_in_date ASC`,
		userID, habitIDs, since,
	)
	if err != nil {
		return nil, fmt.Errorf("habit.ListCheckIns: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var c CheckIn
		if err := rows.Scan(&c.ID, &c.HabitID, &c.UserID, &c.Date, &c.Value,
			&c.TargetAt, &c.Satisfied, &c.CreatedAt, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("habit.ListCheckIns scan: %w", err)
		}
		out[c.HabitID] = append(out[c.HabitID], c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("habit.ListCheckIns rows: %w", err)
	}
	return out, nil
}

// UserTimezone reads the caller's IANA zone. COALESCE guards a NULL column; the
// service separately falls back to UTC when the stored value no longer resolves,
// so a tzdata change can never lock a user out of their own habit.
func (r *pgRepo) UserTimezone(ctx context.Context, userID string) (string, error) {
	var tz string
	err := r.db.QueryRow(ctx,
		`SELECT COALESCE(timezone, 'UTC') FROM users WHERE id = $1 AND deleted_at IS NULL`,
		userID,
	).Scan(&tz)
	if errors.Is(err, pgx.ErrNoRows) {
		return "UTC", nil
	}
	if err != nil {
		return "", fmt.Errorf("habit.UserTimezone: %w", err)
	}
	return tz, nil
}

// Delete removes a habit outright. Its check-ins go with it through the
// ON DELETE CASCADE on habit_check_ins — the streak record is destroyed, which
// is the difference between this and Archive and the reason the caller is
// expected to confirm first.
//
// ok=false when no row matched: missing, or another user's.
func (r *pgRepo) Delete(ctx context.Context, userID, id string) (bool, error) {
	tag, err := r.db.Exec(ctx,
		`DELETE FROM habits WHERE id = $1 AND user_id = $2`,
		id, userID,
	)
	if err != nil {
		return false, fmt.Errorf("habit.Delete: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// CountActive backs the free-plan limit. Archived rows are excluded, which is
// what lets a user archive one habit to make room for another.
func (r *pgRepo) CountActive(ctx context.Context, userID string) (int, error) {
	var n int
	err := r.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM habits WHERE user_id = $1 AND archived_at IS NULL`,
		userID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("habit.CountActive: %w", err)
	}
	return n, nil
}
