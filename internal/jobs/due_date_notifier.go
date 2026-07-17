// Package jobs holds scheduled background jobs invoked by an external scheduler
// (a Render Cron Job) through protected internal endpoints — not in-process
// tickers, so they survive restarts and don't double-fire across instances.
package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
	"github.com/nicoflow/nicoflow-api/pkg/emailutil"
)

// scheduledForLayout is the date format stored in tasks.scheduled_for (ISO date),
// matching the task domain's own layout.
const scheduledForLayout = "2006-01-02"

// RemindableUser is a user eligible for the sweep: their timezone and reminder
// lead time (from notification_preferences, defaulted when no row exists), plus
// the fields the Pro email digest gates on (plan + email + digest preference).
type RemindableUser struct {
	UserID           string
	Email            string
	Plan             string
	Timezone         string
	BeforeDueMinutes int
	EmailDigest      bool
	// Per-family toggles (NIC-1591). Each sweep honours its own; an absent
	// preferences row COALESCEs all to TRUE (every family on by default).
	OverdueEnabled      bool
	DailySummaryEnabled bool
	InboxNudgesEnabled  bool
	StreaksEnabled      bool
	// Reminder hours (NIC-1627). MorningHour drives the day-start/inbox/overdue/
	// due-notify sweeps; EveningHour drives the summary sweep. An absent
	// preferences row COALESCEs to the 8/20 defaults.
	MorningHour int
	EveningHour int
}

// DueTask is the minimal task shape the sweep needs to build a notification.
type DueTask struct {
	ID    string
	Title string
}

// Repository is the data access the sweep needs. Defined here (the consumer)
// per the project's interface-ownership rule.
type Repository interface {
	// ListRemindableUsers returns every non-deleted user with their timezone and
	// effective before_due_minutes (LEFT JOIN prefs, COALESCE to the default).
	ListRemindableUsers(ctx context.Context) ([]RemindableUser, error)
	// ListTasksScheduledOn returns a user's non-terminal tasks whose scheduled_for
	// equals the given ISO date.
	ListTasksScheduledOn(ctx context.Context, userID, isoDate string) ([]DueTask, error)
	// ListOverdueTasks returns a user's non-terminal tasks whose scheduled_for is
	// strictly before the given local ISO date (i.e. already past due).
	ListOverdueTasks(ctx context.Context, userID, localDate string) ([]DueTask, error)
	// HasActiveWork reports whether a user has any open task (non-terminal) or any
	// unprocessed inbox item — the "has something to plan" precondition for the
	// day-plan nudge.
	HasActiveWork(ctx context.Context, userID string) (bool, error)
	// CountUnprocessedInbox returns how many unprocessed items sit in a user's
	// inbox (bucket rows with processed_at IS NULL).
	CountUnprocessedInbox(ctx context.Context, userID string) (int, error)
	// HasStaleInbox reports whether the user has any unprocessed inbox item captured
	// strictly before cutoff — a capture that has gone stale.
	HasStaleInbox(ctx context.Context, userID string, cutoff time.Time) (bool, error)
	// CountCompletedOn returns how many of the user's tasks were completed on the
	// given local ISO date (completed_at bucketed into the user's timezone).
	CountCompletedOn(ctx context.Context, userID, tz, localDate string) (int, error)
	// RecentCompletionDates returns the distinct local ISO dates (user's timezone,
	// descending) on which the user completed at least one task, on or before
	// localDate, limited to a recent window — enough to compute the current streak.
	RecentCompletionDates(ctx context.Context, userID, tz, localDate string, limit int) ([]string, error)
}

// creator is the notification funnel (notification.Service). Narrowed to just the
// method the sweep uses so tests can fake it.
type creator interface {
	Create(ctx context.Context, n notification.Notification) (notification.NotificationView, bool, error)
}

// digestSender delivers the Pro due-task digest email. Satisfied by emailutil in
// production; faked in tests. An empty DSN is handled inside the sender as a no-op.
type digestSender func(to string, tasks []emailutil.DigestTask, smtpDSN string) error

// planPro is the plan value that unlocks the email digest.
const planPro = "pro"

// DueDateNotifier generates task_due_soon notifications, timezone-correct and
// idempotent, for the hourly sweep, and sends the Pro email digest.
type DueDateNotifier struct {
	repo       Repository
	creator    creator
	sendDigest digestSender
	smtpDSN    string
	now        func() time.Time // injectable clock for tests
}

// NewDueDateNotifier builds the sweep job. Pass notification.Service as the
// creator and the configured SMTP DSN (empty ⇒ digest send is a no-op).
func NewDueDateNotifier(repo Repository, c creator, smtpDSN string) *DueDateNotifier {
	return &DueDateNotifier{
		repo:       repo,
		creator:    c,
		sendDigest: emailutil.SendDueDigest,
		smtpDSN:    smtpDSN,
		now:        time.Now,
	}
}

// Run executes one sweep and returns a breakdown of what happened (considered /
// fired / skipped-by-reason). It is safe to re-run within the same hour:
// idempotency is guaranteed by the notification dedupe_key. When dryRun is true it
// computes the breakdown but inserts nothing and sends no digest.
func (n *DueDateNotifier) Run(ctx context.Context, dryRun bool) (*SweepBreakdown, error) {
	const sweep = "due-date"
	users, err := n.repo.ListRemindableUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("jobs.DueDateNotifier: list users: %w", err)
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

		target := reminderTargetDate(local, u.BeforeDueMinutes)
		tasks, err := n.repo.ListTasksScheduledOn(ctx, u.UserID, target)
		if err != nil {
			// One user's failure shouldn't abort the whole sweep; log and continue.
			log.Error().Err(err).Str("user_id", u.UserID).Msg("due-date sweep: list tasks failed")
			continue
		}

		for _, t := range tasks {
			if dryRun {
				continue
			}
			_, inserted, err := n.creator.Create(ctx, notification.Notification{
				UserID:    u.UserID,
				Type:      notification.TypeTaskDueSoon,
				Title:     t.Title,
				Body:      "This task is scheduled soon.",
				DedupeKey: dedupeKey(t.ID, target),
			})
			if err != nil {
				log.Error().Err(err).Str("user_id", u.UserID).Str("task_id", t.ID).Msg("due-date sweep: create failed")
				continue
			}
			if inserted {
				b.Fired++
			}
		}

		// Pro email digest: one batched summary of this user's due-soon tasks, gated
		// on plan + the email_digest preference. Isolated — a send failure logs and
		// continues, never aborting the sweep. Suppressed on dry runs.
		if !dryRun {
			n.maybeSendDigest(u, tasks)
		}
	}
	return b, nil
}

// maybeSendDigest emails a Pro user with email_digest on a batched summary of
// their due-soon tasks. The email-channel gate is the single source of truth
// (notification.ShouldSendEmail) — no local plan/pref logic. Free users and Pro
// users who opted out are skipped; an empty task list or unset SMTP DSN is a no-op.
func (n *DueDateNotifier) maybeSendDigest(u RemindableUser, tasks []DueTask) {
	if !notification.ShouldSendEmail(u.Plan, u.EmailDigest) || len(tasks) == 0 {
		return
	}
	digestTasks := make([]emailutil.DigestTask, len(tasks))
	for i, t := range tasks {
		digestTasks[i] = emailutil.DigestTask{Title: t.Title}
	}
	if err := n.sendDigest(u.Email, digestTasks, n.smtpDSN); err != nil {
		log.Error().Err(err).Str("user_id", u.UserID).Msg("due-date sweep: digest send failed")
	}
}

// reminderTargetDate returns the local ISO target date for a due-soon reminder:
// the user's local day plus the lead time (beforeDueMinutes, default 1440 =
// tomorrow), so a 24h lead targets the next local day. The caller has already
// resolved local and confirmed the fire window.
func reminderTargetDate(local time.Time, beforeDueMinutes int) string {
	if beforeDueMinutes < 0 {
		beforeDueMinutes = 0
	}
	leadDays := beforeDueMinutes / 1440
	return local.AddDate(0, 0, leadDays).Format(scheduledForLayout)
}

// dedupeKey builds the idempotency key for a due-soon notification. Re-running the
// sweep for the same task/date collides on the notifications (user_id, dedupe_key)
// unique index → ON CONFLICT DO NOTHING.
func dedupeKey(taskID, isoDate string) *string {
	k := fmt.Sprintf("%s:%s:%s", notification.TypeTaskDueSoon, taskID, isoDate)
	return &k
}
