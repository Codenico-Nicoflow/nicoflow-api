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

// reminderLocalHour is the local hour (24h) at which a user's due-soon reminders
// fire. The hourly sweep only acts on users whose current local hour equals this.
const reminderLocalHour = 8

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

// Run executes one sweep and returns how many notifications were newly created
// (duplicates skipped by dedupe_key are not counted). It is safe to re-run within
// the same hour: idempotency is guaranteed by the notification dedupe_key.
func (n *DueDateNotifier) Run(ctx context.Context) (int, error) {
	users, err := n.repo.ListRemindableUsers(ctx)
	if err != nil {
		return 0, fmt.Errorf("jobs.DueDateNotifier: list users: %w", err)
	}

	nowUTC := n.now().UTC()
	generated := 0

	for _, u := range users {
		target, ok := reminderTargetDate(nowUTC, u.Timezone, u.BeforeDueMinutes)
		if !ok {
			continue // not this user's 08:00 hour (or a bad timezone) → skip
		}

		tasks, err := n.repo.ListTasksScheduledOn(ctx, u.UserID, target)
		if err != nil {
			// One user's failure shouldn't abort the whole sweep; log and continue.
			log.Error().Err(err).Str("user_id", u.UserID).Msg("due-date sweep: list tasks failed")
			continue
		}

		for _, t := range tasks {
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
				generated++
			}
		}

		// Pro email digest: one batched summary of this user's due-soon tasks, gated
		// on plan + the email_digest preference. Isolated — a send failure logs and
		// continues, never aborting the sweep.
		n.maybeSendDigest(u, tasks)
	}
	return generated, nil
}

// maybeSendDigest emails a Pro user with email_digest on a batched summary of
// their due-soon tasks. Free users and Pro users who opted out are skipped; an
// empty task list or unset SMTP DSN is a no-op handled by the sender.
func (n *DueDateNotifier) maybeSendDigest(u RemindableUser, tasks []DueTask) {
	if u.Plan != planPro || !u.EmailDigest || len(tasks) == 0 {
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

// reminderTargetDate reports whether the user should be reminded in this hour and,
// if so, the local ISO target date. A user fires only when their local hour equals
// reminderLocalHour; the target date is their local today plus the lead time
// (beforeDueMinutes, default 1440 = tomorrow), so a 24h lead at 08:00 targets the
// next local day. An unparseable timezone is skipped (ok=false).
func reminderTargetDate(nowUTC time.Time, tz string, beforeDueMinutes int) (string, bool) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "", false
	}
	local := nowUTC.In(loc)
	if local.Hour() != reminderLocalHour {
		return "", false
	}
	if beforeDueMinutes < 0 {
		beforeDueMinutes = 0
	}
	leadDays := beforeDueMinutes / 1440
	target := local.AddDate(0, 0, leadDays)
	return target.Format(scheduledForLayout), true
}

// dedupeKey builds the idempotency key for a due-soon notification. Re-running the
// sweep for the same task/date collides on the notifications (user_id, dedupe_key)
// unique index → ON CONFLICT DO NOTHING.
func dedupeKey(taskID, isoDate string) *string {
	k := fmt.Sprintf("%s:%s:%s", notification.TypeTaskDueSoon, taskID, isoDate)
	return &k
}
