package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgRepository struct {
	db *pgxpool.Pool
}

// NewRepository returns a PostgreSQL-backed Repository for the sweep jobs.
func NewRepository(db *pgxpool.Pool) Repository {
	return &pgRepository{db: db}
}

// ListRemindableUsers returns every active (non-deleted) user with their timezone,
// effective before_due_minutes, plan, email, email_digest, and the four per-family
// sweep toggles (overdue / daily_summary / inbox / streaks). Users with no
// preferences row fall back to the defaults (lead 1440, all toggles on) via
// LEFT JOIN + COALESCE.
func (r *pgRepository) ListRemindableUsers(ctx context.Context) ([]RemindableUser, error) {
	rows, err := r.db.Query(ctx, `
		SELECT u.id, u.email, u.plan, u.timezone,
		       COALESCE(p.before_due_minutes, 1440),
		       COALESCE(p.email_digest, TRUE),
		       COALESCE(p.overdue_enabled, TRUE),
		       COALESCE(p.daily_summary_enabled, TRUE),
		       COALESCE(p.inbox_nudges_enabled, TRUE),
		       COALESCE(p.streaks_enabled, TRUE),
		       COALESCE(p.morning_hour, 8),
		       COALESCE(p.evening_hour, 20)
		FROM users u
		LEFT JOIN notification_preferences p ON p.user_id = u.id
		WHERE u.deleted_at IS NULL`)
	if err != nil {
		return nil, fmt.Errorf("jobs.ListRemindableUsers: %w", err)
	}
	defer rows.Close()

	var out []RemindableUser
	for rows.Next() {
		var u RemindableUser
		if err := rows.Scan(&u.UserID, &u.Email, &u.Plan, &u.Timezone, &u.BeforeDueMinutes, &u.EmailDigest,
			&u.OverdueEnabled, &u.DailySummaryEnabled, &u.InboxNudgesEnabled, &u.StreaksEnabled,
			&u.MorningHour, &u.EveningHour); err != nil {
			return nil, fmt.Errorf("jobs.ListRemindableUsers scan: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// HasActiveWork reports whether the user has anything to plan: at least one
// non-terminal task, or at least one unprocessed inbox item. Used as the
// day-plan nudge precondition (only nudge users who actually have open work).
func (r *pgRepository) HasActiveWork(ctx context.Context, userID string) (bool, error) {
	var has bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM tasks
			WHERE user_id = @userID AND status NOT IN ('done', 'cancelled')
			UNION ALL
			SELECT 1 FROM bucket
			WHERE user_id = @userID AND processed_at IS NULL
		)`,
		pgx.NamedArgs{"userID": userID},
	).Scan(&has)
	if err != nil {
		return false, fmt.Errorf("jobs.HasActiveWork: %w", err)
	}
	return has, nil
}

// ListOverdueTasks returns a user's non-terminal tasks whose scheduled_for is
// strictly before localDate — scheduled in the past and still open. Terminal
// statuses (done, cancelled) and unscheduled tasks (NULL scheduled_for) are
// excluded.
func (r *pgRepository) ListOverdueTasks(ctx context.Context, userID, localDate string) ([]DueTask, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, title
		FROM tasks
		WHERE user_id = @userID
		  AND scheduled_for IS NOT NULL
		  AND scheduled_for < @localDate
		  AND status NOT IN ('done', 'cancelled')`,
		pgx.NamedArgs{"userID": userID, "localDate": localDate},
	)
	if err != nil {
		return nil, fmt.Errorf("jobs.ListOverdueTasks: %w", err)
	}
	defer rows.Close()

	var out []DueTask
	for rows.Next() {
		var t DueTask
		if err := rows.Scan(&t.ID, &t.Title); err != nil {
			return nil, fmt.Errorf("jobs.ListOverdueTasks scan: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CountUnprocessedInbox returns how many unprocessed items sit in a user's inbox
// (bucket rows with processed_at IS NULL).
func (r *pgRepository) CountUnprocessedInbox(ctx context.Context, userID string) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM bucket
		WHERE user_id = @userID AND processed_at IS NULL`,
		pgx.NamedArgs{"userID": userID},
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("jobs.CountUnprocessedInbox: %w", err)
	}
	return n, nil
}

// HasStaleInbox reports whether the user has any unprocessed inbox item captured
// strictly before cutoff — a capture that has gone stale.
func (r *pgRepository) HasStaleInbox(ctx context.Context, userID string, cutoff time.Time) (bool, error) {
	var has bool
	err := r.db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM bucket
			WHERE user_id = @userID AND processed_at IS NULL AND created_at < @cutoff
		)`,
		pgx.NamedArgs{"userID": userID, "cutoff": cutoff},
	).Scan(&has)
	if err != nil {
		return false, fmt.Errorf("jobs.HasStaleInbox: %w", err)
	}
	return has, nil
}

// CountCompletedOn returns how many of the user's tasks were completed on the given
// local ISO date. completed_at (TIMESTAMPTZ) is bucketed into the user's timezone
// before comparing to the date, so "today" respects the user's local day boundary.
func (r *pgRepository) CountCompletedOn(ctx context.Context, userID, tz, localDate string) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM tasks
		WHERE user_id = @userID
		  AND completed_at IS NOT NULL
		  AND (completed_at AT TIME ZONE @tz)::date = @localDate::date`,
		pgx.NamedArgs{"userID": userID, "tz": tz, "localDate": localDate},
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("jobs.CountCompletedOn: %w", err)
	}
	return n, nil
}

// RecentCompletionDates returns the distinct local ISO dates (user's timezone,
// descending) on which the user completed at least one task, on or before localDate,
// capped at limit — enough history to compute the current daily-completion streak.
func (r *pgRepository) RecentCompletionDates(ctx context.Context, userID, tz, localDate string, limit int) ([]string, error) {
	rows, err := r.db.Query(ctx, `
		SELECT DISTINCT (completed_at AT TIME ZONE @tz)::date AS d
		FROM tasks
		WHERE user_id = @userID
		  AND completed_at IS NOT NULL
		  AND (completed_at AT TIME ZONE @tz)::date <= @localDate::date
		ORDER BY d DESC
		LIMIT @limit`,
		pgx.NamedArgs{"userID": userID, "tz": tz, "localDate": localDate, "limit": limit},
	)
	if err != nil {
		return nil, fmt.Errorf("jobs.RecentCompletionDates: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var d time.Time
		if err := rows.Scan(&d); err != nil {
			return nil, fmt.Errorf("jobs.RecentCompletionDates scan: %w", err)
		}
		out = append(out, d.Format(scheduledForLayout))
	}
	return out, rows.Err()
}

// ListTasksScheduledOn returns a user's non-terminal tasks scheduled for the given
// ISO date. Terminal statuses (done, cancelled) are excluded — no reminder for work
// already closed out.
func (r *pgRepository) ListTasksScheduledOn(ctx context.Context, userID, isoDate string) ([]DueTask, error) {
	rows, err := r.db.Query(ctx, `
		SELECT id, title
		FROM tasks
		WHERE user_id = @userID
		  AND scheduled_for = @isoDate
		  AND status NOT IN ('done', 'cancelled')`,
		pgx.NamedArgs{"userID": userID, "isoDate": isoDate},
	)
	if err != nil {
		return nil, fmt.Errorf("jobs.ListTasksScheduledOn: %w", err)
	}
	defer rows.Close()

	var out []DueTask
	for rows.Next() {
		var t DueTask
		if err := rows.Scan(&t.ID, &t.Title); err != nil {
			return nil, fmt.Errorf("jobs.ListTasksScheduledOn scan: %w", err)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
