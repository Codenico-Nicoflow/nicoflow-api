package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

// DayStartNotifier gives each user a start-of-day cue at their local 08:00:
//   - task_scheduled_today (FREE, all plans): one summary "N tasks scheduled today"
//     when N > 0 — a single notification, never one per task, to avoid a flood.
//   - day_plan_nudge (Pro only): when nothing is scheduled today but the user still
//     has open work, nudge them to plan their day.
//
// Both outputs are idempotent per user per local day (dedupe_key), so the hourly
// sweep is safe to re-run.
type DayStartNotifier struct {
	repo    Repository
	creator creator
	now     func() time.Time // injectable clock for tests
}

// NewDayStartNotifier builds the start-of-day sweep. Pass notification.Service as
// the creator (the same funnel the other sweeps use).
func NewDayStartNotifier(repo Repository, c creator) *DayStartNotifier {
	return &DayStartNotifier{repo: repo, creator: c, now: time.Now}
}

// Run executes one sweep and returns a breakdown of what happened. Safe to re-run
// within the same local day: idempotency is guaranteed by each output's
// dedupe_key. When dryRun is true it computes the breakdown but inserts nothing.
func (n *DayStartNotifier) Run(ctx context.Context, dryRun bool) (*SweepBreakdown, error) {
	const sweep = "day-start"
	users, err := n.repo.ListRemindableUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("jobs.DayStartNotifier: list users: %w", err)
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

		localToday := local.Format(scheduledForLayout)
		tasks, err := n.repo.ListTasksScheduledOn(ctx, u.UserID, localToday)
		if err != nil {
			// One user's failure must not abort the whole sweep; log and continue.
			log.Error().Err(err).Str("user_id", u.UserID).Msg("day-start sweep: list tasks failed")
			continue
		}

		if len(tasks) > 0 {
			if !dryRun && n.emitScheduledSummary(ctx, u, localToday, len(tasks)) {
				b.Fired++
			}
			continue // has work scheduled → no nudge
		}

		if !dryRun && n.emitPlanNudge(ctx, u, localToday) {
			b.Fired++
		}
	}
	return b, nil
}

// emitScheduledSummary creates the FREE one-line "N tasks scheduled today" summary
// (all plans). Returns whether a new notification row was inserted.
func (n *DayStartNotifier) emitScheduledSummary(ctx context.Context, u RemindableUser, localToday string, count int) bool {
	_, inserted, err := n.creator.Create(ctx, notification.Notification{
		UserID:    u.UserID,
		Type:      notification.TypeTaskScheduledToday,
		Title:     "Your day ahead",
		Body:      fmt.Sprintf("You have %d %s scheduled today.", count, plural(count, "task", "tasks")),
		Metadata:  scheduledCountMeta(count),
		DedupeKey: notification.DedupeTaskScheduledToday(u.UserID, localToday),
	})
	if err != nil {
		log.Error().Err(err).Str("user_id", u.UserID).Msg("day-start sweep: summary create failed")
		return false
	}
	return inserted
}

// emitPlanNudge creates the Pro day_plan_nudge — only for Pro users who have open
// work but nothing scheduled today. Free users (and Pro users with no work) get
// nothing. Returns whether a new notification row was inserted.
func (n *DayStartNotifier) emitPlanNudge(ctx context.Context, u RemindableUser, localToday string) bool {
	if !notification.IsProType(notification.TypeNothingScheduled) || u.Plan != planPro {
		return false // Pro-only; free users get no nudge
	}

	hasWork, err := n.repo.HasActiveWork(ctx, u.UserID)
	if err != nil {
		log.Error().Err(err).Str("user_id", u.UserID).Msg("day-start sweep: active-work check failed")
		return false
	}
	if !hasWork {
		return false // nothing to plan → stay silent
	}

	_, inserted, err := n.creator.Create(ctx, notification.Notification{
		UserID:    u.UserID,
		Type:      notification.TypeNothingScheduled,
		Title:     "Plan your day",
		Body:      "Nothing is scheduled for today — take a moment to plan.",
		DedupeKey: notification.DedupeDayPlanNudge(u.UserID, localToday),
	})
	if err != nil {
		log.Error().Err(err).Str("user_id", u.UserID).Msg("day-start sweep: nudge create failed")
		return false
	}
	return inserted
}

// scheduledCountMeta encodes the summary count so the client can render it without
// parsing the body. Marshalling a fixed-shape struct never fails, so the error is
// dropped deliberately.
func scheduledCountMeta(count int) json.RawMessage {
	b, _ := json.Marshal(struct {
		Count int `json:"count"`
	}{Count: count})
	return b
}

// plural picks the singular or plural form for a count.
func plural(count int, singular, pluralForm string) string {
	if count == 1 {
		return singular
	}
	return pluralForm
}
