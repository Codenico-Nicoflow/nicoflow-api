package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

// inboxUnprocessedThreshold is the minimum unprocessed-inbox count that triggers
// the daily "process your inbox" nudge. Below it we stay quiet — a couple of
// captures is normal, a pile is the coaching signal.
const inboxUnprocessedThreshold = 5

// inboxStaleAfterDays is how old (in days) an unprocessed capture must be before
// it counts as stale and earns the weekly warning.
const inboxStaleAfterDays = 7

// InboxNotifier coaches the capture→process habit with two Pro nudges at each
// user's local reminder hour:
//   - inbox_unprocessed (Pro): when the unprocessed count is at/above the
//     threshold, one "N unprocessed inbox items" reminder per local day.
//   - inbox_stale (Pro): when a capture has sat unprocessed past the stale age,
//     one warning per ISO week (weekly, so we don't nag every morning).
//
// Both are Pro-only proactive types; free users are skipped. Idempotent via
// dedupe_key, so the hourly sweep is safe to re-run.
type InboxNotifier struct {
	repo    Repository
	creator creator
	now     func() time.Time // injectable clock for tests
}

// NewInboxNotifier builds the inbox sweep. Pass notification.Service as the creator.
func NewInboxNotifier(repo Repository, c creator) *InboxNotifier {
	return &InboxNotifier{repo: repo, creator: c, now: time.Now}
}

// Run executes one sweep and returns how many notifications were newly created
// (dedupe-suppressed duplicates not counted). Safe to re-run within the same
// window: idempotency is guaranteed by each output's dedupe_key.
func (n *InboxNotifier) Run(ctx context.Context) (int, error) {
	users, err := n.repo.ListRemindableUsers(ctx)
	if err != nil {
		return 0, fmt.Errorf("jobs.InboxNotifier: list users: %w", err)
	}

	nowUTC := n.now().UTC()
	generated := 0

	for _, u := range users {
		local, ok := inboxLocalTime(nowUTC, u.Timezone)
		if !ok {
			continue // not this user's reminder hour (or a bad timezone) → skip
		}
		if !notification.IsProType(notification.TypeInboxUnprocessed) || u.Plan != planPro {
			continue // both inbox nudges are Pro-only
		}
		if !u.InboxNudgesEnabled {
			continue // user opted out of inbox nudges
		}

		if n.emitUnprocessed(ctx, u, local) {
			generated++
		}
		if n.emitStale(ctx, u, local) {
			generated++
		}
	}
	return generated, nil
}

// emitUnprocessed creates the daily inbox_unprocessed nudge when the user's
// unprocessed count is at/above the threshold. Returns whether a row was inserted.
func (n *InboxNotifier) emitUnprocessed(ctx context.Context, u RemindableUser, local time.Time) bool {
	count, err := n.repo.CountUnprocessedInbox(ctx, u.UserID)
	if err != nil {
		log.Error().Err(err).Str("user_id", u.UserID).Msg("inbox sweep: count unprocessed failed")
		return false
	}
	if count < inboxUnprocessedThreshold {
		return false
	}

	localDate := local.Format(scheduledForLayout)
	_, inserted, err := n.creator.Create(ctx, notification.Notification{
		UserID:    u.UserID,
		Type:      notification.TypeInboxUnprocessed,
		Title:     "Inbox is filling up",
		Body:      fmt.Sprintf("You have %d unprocessed inbox %s.", count, plural(count, "item", "items")),
		Metadata:  inboxCountMeta(count),
		DedupeKey: notification.DedupeInboxUnprocessed(u.UserID, localDate),
	})
	if err != nil {
		log.Error().Err(err).Str("user_id", u.UserID).Msg("inbox sweep: unprocessed create failed")
		return false
	}
	return inserted
}

// emitStale creates the weekly inbox_stale warning when the user has an unprocessed
// capture older than the stale age. Returns whether a row was inserted.
func (n *InboxNotifier) emitStale(ctx context.Context, u RemindableUser, local time.Time) bool {
	cutoff := local.AddDate(0, 0, -inboxStaleAfterDays)
	hasStale, err := n.repo.HasStaleInbox(ctx, u.UserID, cutoff)
	if err != nil {
		log.Error().Err(err).Str("user_id", u.UserID).Msg("inbox sweep: stale check failed")
		return false
	}
	if !hasStale {
		return false
	}

	_, inserted, err := n.creator.Create(ctx, notification.Notification{
		UserID:    u.UserID,
		Type:      notification.TypeInboxStale,
		Title:     "Stale captures in your inbox",
		Body:      "Some inbox items have been sitting for a while — process or clear them.",
		DedupeKey: notification.DedupeInboxStale(u.UserID, isoWeek(local)),
	})
	if err != nil {
		log.Error().Err(err).Str("user_id", u.UserID).Msg("inbox sweep: stale create failed")
		return false
	}
	return inserted
}

// inboxLocalTime reports whether the user should be swept this hour and, if so,
// their local time. A user fires only when their local hour equals
// reminderLocalHour; an unparseable timezone is skipped (ok=false).
func inboxLocalTime(nowUTC time.Time, tz string) (time.Time, bool) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Time{}, false
	}
	local := nowUTC.In(loc)
	if local.Hour() != reminderLocalHour {
		return time.Time{}, false
	}
	return local, true
}

// isoWeek renders a time as an ISO year-week key (e.g. "2026-W29") — the bucket the
// stale warning dedupes on so it fires at most once per week.
func isoWeek(t time.Time) string {
	year, week := t.ISOWeek()
	return fmt.Sprintf("%04d-W%02d", year, week)
}

// inboxCountMeta encodes the unprocessed count so the client can render it without
// parsing the body. Marshalling a fixed-shape struct never fails.
func inboxCountMeta(count int) json.RawMessage {
	b, _ := json.Marshal(struct {
		Count int `json:"count"`
	}{Count: count})
	return b
}
