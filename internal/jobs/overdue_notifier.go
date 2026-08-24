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

// Run executes one sweep and returns a breakdown of what happened. Safe to re-run
// within the same local day: idempotency is guaranteed by the task_overdue
// dedupe_key. When dryRun is true it computes the breakdown but inserts nothing.
func (n *OverdueNotifier) Run(ctx context.Context, dryRun bool) (*SweepBreakdown, error) {
	const sweep = "overdue"
	users, err := n.repo.ListRemindableUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("jobs.OverdueNotifier: list users: %w", err)
	}

	nowUTC := n.now().UTC()
	b := newBreakdown()

	for _, u := range users {
		b.Considered++

		local, ok := localHour(nowUTC, u.Timezone)
		if !ok {
			b.skip(sweep, skipBadTimezone, u.UserID, u.Timezone)
			continue
		}
		if !inFireWindow(local.Hour(), u.MorningHour) {
			b.skip(sweep, skipOutsideWindow, u.UserID, u.Timezone)
			continue
		}
		if !u.OverdueEnabled {
			b.skip(sweep, skipToggleOff, u.UserID, u.Timezone)
			continue
		}

		localToday := local.Format(scheduledForLayout)
		tasks, err := n.repo.ListOverdueTasks(ctx, u.UserID, localToday)
		if err != nil {
			// One user's failure must not abort the whole sweep; log and continue.
			log.Error().Err(err).Str("user_id", u.UserID).Msg("overdue sweep: list tasks failed")
			continue
		}

		for _, t := range tasks {
			if dryRun {
				continue
			}
			_, inserted, err := n.creator.Create(ctx, notification.Notification{
				UserID:    u.UserID,
				Type:      notification.TypeTaskOverdue,
				Title:     t.Title,
				Body:      "This task is overdue.",
				Metadata:  taskReminderMeta(t.ID, t.ProjectID),
				DedupeKey: notification.DedupeTaskOverdue(t.ID, localToday),
			})
			if err != nil {
				log.Error().Err(err).Str("user_id", u.UserID).Str("task_id", t.ID).Msg("overdue sweep: create failed")
				continue
			}
			if inserted {
				b.Fired++
			}
		}
	}
	return b, nil
}
