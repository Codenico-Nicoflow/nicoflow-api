package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

// OverdueNotifier generates task_overdue notifications for tasks scheduled in the
// past that are still open. FREE essential reminder: fires for all plans, once
// per task per local day, at each user's local reminder hour. Mirrors
// DueDateNotifier's timezone gating + per-user isolation + dedupe idempotency.
type OverdueNotifier struct {
	repo    Repository
	creator creator
	now     func() time.Time // injectable clock for tests
}

// NewOverdueNotifier builds the overdue sweep. Pass notification.Service as the
// creator (the same funnel the due-date sweep uses).
func NewOverdueNotifier(repo Repository, c creator) *OverdueNotifier {
	return &OverdueNotifier{repo: repo, creator: c, now: time.Now}
}

// Run executes one sweep and returns how many notifications were newly created
// (dedupe-suppressed duplicates not counted). Safe to re-run within the same
// local day: idempotency is guaranteed by the task_overdue dedupe_key.
func (n *OverdueNotifier) Run(ctx context.Context) (int, error) {
	users, err := n.repo.ListRemindableUsers(ctx)
	if err != nil {
		return 0, fmt.Errorf("jobs.OverdueNotifier: list users: %w", err)
	}

	nowUTC := n.now().UTC()
	generated := 0

	for _, u := range users {
		localToday, ok := overdueLocalToday(nowUTC, u.Timezone)
		if !ok {
			continue // not this user's reminder hour (or a bad timezone) → skip
		}

		tasks, err := n.repo.ListOverdueTasks(ctx, u.UserID, localToday)
		if err != nil {
			// One user's failure must not abort the whole sweep; log and continue.
			log.Error().Err(err).Str("user_id", u.UserID).Msg("overdue sweep: list tasks failed")
			continue
		}

		for _, t := range tasks {
			_, inserted, err := n.creator.Create(ctx, notification.Notification{
				UserID:    u.UserID,
				Type:      notification.TypeTaskOverdue,
				Title:     t.Title,
				Body:      "This task is overdue.",
				DedupeKey: notification.DedupeTaskOverdue(t.ID, localToday),
			})
			if err != nil {
				log.Error().Err(err).Str("user_id", u.UserID).Str("task_id", t.ID).Msg("overdue sweep: create failed")
				continue
			}
			if inserted {
				generated++
			}
		}
	}
	return generated, nil
}

// overdueLocalToday reports whether the user should be swept this hour and, if so,
// their local ISO "today". A user fires only when their local hour equals
// reminderLocalHour; overdue = scheduled_for strictly before this date. An
// unparseable timezone is skipped (ok=false).
func overdueLocalToday(nowUTC time.Time, tz string) (string, bool) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "", false
	}
	local := nowUTC.In(loc)
	if local.Hour() != reminderLocalHour {
		return "", false
	}
	return local.Format(scheduledForLayout), true
}
