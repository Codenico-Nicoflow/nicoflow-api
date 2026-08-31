// Package jobs holds scheduled background jobs invoked by an external scheduler
// (a Render Cron Job) through protected internal endpoints — not in-process
// tickers, so they survive restarts and don't double-fire across instances.
package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// scheduledForLayout is the date format stored in tasks.scheduled_for (ISO date),
// matching the task domain's own layout.
const scheduledForLayout = "2006-01-02"

// RemindableUser is a user eligible for a digest sweep: their timezone and the
// fields the two daily digests gate on (plan, reminder hours, per-digest toggle).
type RemindableUser struct {
	UserID   string
	Plan     string
	Timezone string
	// Per-digest toggles. An absent preferences row COALESCEs both to TRUE.
	MorningDigestEnabled bool
	EveningDigestEnabled bool
	StreaksEnabled       bool
	MorningHour          int
	EveningHour          int
}

// Repository is the data access the digest sweeps need. Defined here (the
// consumer) per the project's interface-ownership rule.
type Repository interface {
	// ListRemindableUsers returns every non-deleted user with their timezone and
	// effective preferences (LEFT JOIN prefs, COALESCE to defaults).
	ListRemindableUsers(ctx context.Context) ([]RemindableUser, error)
	// CountScheduledOn returns how many of a user's non-terminal tasks are
	// scheduled for the given ISO date.
	CountScheduledOn(ctx context.Context, userID, isoDate string) (int, error)
	// CountOverdue returns how many of a user's non-terminal tasks have
	// scheduled_for strictly before the given local ISO date.
	CountOverdue(ctx context.Context, userID, localDate string) (int, error)
	// CountUnprocessedInbox returns how many unprocessed items sit in a user's
	// inbox (bucket rows with processed_at IS NULL).
	CountUnprocessedInbox(ctx context.Context, userID string) (int, error)
	// CountOpenTasks returns how many of a user's tasks are still non-terminal
	// (status not in done/cancelled), regardless of schedule.
	CountOpenTasks(ctx context.Context, userID string) (int, error)
	// CountCompletedOn returns how many of the user's tasks were completed on the
	// given local ISO date (completed_at bucketed into the user's timezone).
	CountCompletedOn(ctx context.Context, userID, tz, localDate string) (int, error)
	// RecentCompletionDates returns the distinct local ISO dates (user's timezone,
	// descending) on which the user completed at least one task, on or before
	// localDate, limited to a recent window — enough to compute the current streak.
	RecentCompletionDates(ctx context.Context, userID, tz, localDate string, limit int) ([]string, error)
}

type pgRepository struct {
	db *pgxpool.Pool
}

// NewRepository returns a PostgreSQL-backed Repository for the digest sweeps.
func NewRepository(db *pgxpool.Pool) Repository {
	return &pgRepository{db: db}
}

// ListRemindableUsers returns every active (non-deleted) user with their timezone,
// plan, and digest preferences. Users with no preferences row fall back to the
// defaults (both digests on, morning 08:00, evening 20:00) via LEFT JOIN + COALESCE.
func (r *pgRepository) ListRemindableUsers(ctx context.Context) ([]RemindableUser, error) {
	rows, err := r.db.Query(ctx, `
		SELECT u.id, u.plan, u.timezone,
		       COALESCE(p.morning_digest_enabled, TRUE),
		       COALESCE(p.evening_digest_enabled, TRUE),
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
		if err := rows.Scan(&u.UserID, &u.Plan, &u.Timezone,
			&u.MorningDigestEnabled, &u.EveningDigestEnabled, &u.StreaksEnabled,
			&u.MorningHour, &u.EveningHour); err != nil {
			return nil, fmt.Errorf("jobs.ListRemindableUsers scan: %w", err)
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CountScheduledOn returns how many of a user's non-terminal tasks are scheduled
// for isoDate.
func (r *pgRepository) CountScheduledOn(ctx context.Context, userID, isoDate string) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM tasks
		WHERE user_id = @userID
		  AND scheduled_for = @isoDate
		  AND status NOT IN ('done', 'cancelled')`,
		pgx.NamedArgs{"userID": userID, "isoDate": isoDate},
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("jobs.CountScheduledOn: %w", err)
	}
	return n, nil
}

// CountOverdue returns how many of a user's non-terminal tasks have scheduled_for
// strictly before localDate — scheduled in the past and still open.
func (r *pgRepository) CountOverdue(ctx context.Context, userID, localDate string) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM tasks
		WHERE user_id = @userID
		  AND scheduled_for IS NOT NULL
		  AND scheduled_for < @localDate
		  AND status NOT IN ('done', 'cancelled')`,
		pgx.NamedArgs{"userID": userID, "localDate": localDate},
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("jobs.CountOverdue: %w", err)
	}
	return n, nil
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

// CountOpenTasks returns how many of a user's tasks are still non-terminal,
// regardless of schedule — the evening digest's "N tasks left" figure.
func (r *pgRepository) CountOpenTasks(ctx context.Context, userID string) (int, error) {
	var n int
	err := r.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM tasks
		WHERE user_id = @userID AND status NOT IN ('done', 'cancelled')`,
		pgx.NamedArgs{"userID": userID},
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("jobs.CountOpenTasks: %w", err)
	}
	return n, nil
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
