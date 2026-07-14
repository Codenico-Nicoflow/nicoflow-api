package jobs

import (
	"context"
	"fmt"

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
// effective before_due_minutes, plan, email, and email_digest preference. Users
// with no preferences row fall back to the defaults (lead 1440, digest on) via
// LEFT JOIN + COALESCE.
func (r *pgRepository) ListRemindableUsers(ctx context.Context) ([]RemindableUser, error) {
	rows, err := r.db.Query(ctx, `
		SELECT u.id, u.email, u.plan, u.timezone,
		       COALESCE(p.before_due_minutes, 1440),
		       COALESCE(p.email_digest, TRUE)
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
		if err := rows.Scan(&u.UserID, &u.Email, &u.Plan, &u.Timezone, &u.BeforeDueMinutes, &u.EmailDigest); err != nil {
			return nil, fmt.Errorf("jobs.ListRemindableUsers scan: %w", err)
		}
		out = append(out, u)
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
