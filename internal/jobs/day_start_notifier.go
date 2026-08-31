package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

// creator is the notification funnel (notification.Service). Narrowed to just the
// method the sweeps use so tests can fake it.
type creator interface {
	Create(ctx context.Context, n notification.Notification) (notification.NotificationView, bool, error)
}

// planPro is the plan value that unlocks Pro-only notification types.
const planPro = "pro"

// DayStartNotifier gives every user (all plans, unified — NIC notification
// rework) a single morning_digest at their local morning hour, rolling together
// what used to be three separate reminder streams (task_due_soon, task_overdue,
// inbox_unprocessed): how many tasks are scheduled today, how many are overdue,
// and how many inbox items are unprocessed. Silent when all three are zero — a
// genuinely empty day gets no ping. Idempotent per user per local day.
type DayStartNotifier struct {
	repo    Repository
	creator creator
	now     func() time.Time // injectable clock for tests
}

// NewDayStartNotifier builds the morning-digest sweep. Pass notification.Service
// as the creator (the same funnel the other sweep uses).
func NewDayStartNotifier(repo Repository, c creator) *DayStartNotifier {
	return &DayStartNotifier{repo: repo, creator: c, now: time.Now}
}

// Run executes one sweep and returns a breakdown of what happened. Safe to re-run
// within the same local day: idempotency is guaranteed by the digest's dedupe_key.
// When dryRun is true it computes the breakdown but inserts nothing.
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
		if !u.MorningDigestEnabled {
			b.skip(sweep, skipToggleOff, u.UserID, u.Timezone)
			continue
		}

		localToday := local.Format(scheduledForLayout)
		counts, err := n.gatherCounts(ctx, u.UserID, localToday)
		if err != nil {
			// One user's failure must not abort the whole sweep; log and continue.
			log.Error().Err(err).Str("user_id", u.UserID).Msg("day-start sweep: gather counts failed")
			continue
		}
		if counts.isEmpty() {
			continue // nothing to report → stay silent
		}
		if dryRun {
			continue
		}
		if n.emitDigest(ctx, u, localToday, counts) {
			b.Fired++
		}
	}
	return b, nil
}

// morningCounts is the digest's three rolled-up figures.
type morningCounts struct {
	Scheduled   int
	Overdue     int
	Unprocessed int
}

func (c morningCounts) isEmpty() bool {
	return c.Scheduled == 0 && c.Overdue == 0 && c.Unprocessed == 0
}

// gatherCounts fetches the three counts a morning digest folds together.
func (n *DayStartNotifier) gatherCounts(ctx context.Context, userID, localToday string) (morningCounts, error) {
	scheduled, err := n.repo.CountScheduledOn(ctx, userID, localToday)
	if err != nil {
		return morningCounts{}, fmt.Errorf("scheduled: %w", err)
	}
	overdue, err := n.repo.CountOverdue(ctx, userID, localToday)
	if err != nil {
		return morningCounts{}, fmt.Errorf("overdue: %w", err)
	}
	unprocessed, err := n.repo.CountUnprocessedInbox(ctx, userID)
	if err != nil {
		return morningCounts{}, fmt.Errorf("unprocessed: %w", err)
	}
	return morningCounts{Scheduled: scheduled, Overdue: overdue, Unprocessed: unprocessed}, nil
}

// emitDigest creates the morning_digest notification. Returns whether a new
// notification row was inserted.
func (n *DayStartNotifier) emitDigest(ctx context.Context, u RemindableUser, localToday string, c morningCounts) bool {
	_, inserted, err := n.creator.Create(ctx, notification.Notification{
		UserID:    u.UserID,
		Type:      notification.TypeMorningDigest,
		Title:     "Plan your day",
		Body:      morningDigestBody(c),
		Metadata:  morningDigestMeta(c),
		DedupeKey: notification.DedupeMorningDigest(u.UserID, localToday),
	})
	if err != nil {
		log.Error().Err(err).Str("user_id", u.UserID).Msg("day-start sweep: digest create failed")
		return false
	}
	return inserted
}

// morningDigestBody composes the human-readable summary line, e.g.
// "3 tasks scheduled today, 1 overdue, 2 unprocessed in your inbox."
func morningDigestBody(c morningCounts) string {
	var parts []string
	if c.Scheduled > 0 {
		parts = append(parts, fmt.Sprintf("%d %s scheduled today", c.Scheduled, plural(c.Scheduled, "task", "tasks")))
	}
	if c.Overdue > 0 {
		parts = append(parts, fmt.Sprintf("%d overdue", c.Overdue))
	}
	if c.Unprocessed > 0 {
		parts = append(parts, fmt.Sprintf("%d unprocessed in your inbox", c.Unprocessed))
	}
	return strings.Join(parts, ", ") + "."
}

// morningDigestMeta encodes the three counts so the client can render them
// without parsing the body.
func morningDigestMeta(c morningCounts) json.RawMessage {
	b, _ := json.Marshal(struct {
		Scheduled   int `json:"scheduled"`
		Overdue     int `json:"overdue"`
		Unprocessed int `json:"unprocessed"`
	}{Scheduled: c.Scheduled, Overdue: c.Overdue, Unprocessed: c.Unprocessed})
	return b
}

// plural picks the singular or plural form for a count.
func plural(count int, singular, pluralForm string) string {
	if count == 1 {
		return singular
	}
	return pluralForm
}
