package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

// streakMilestones are the consecutive-day streak lengths worth celebrating. A
// streak_milestone fires when the current streak exactly equals one of these; the
// per-milestone dedupe key makes it a once-ever event.
var streakMilestones = []int{7, 30, 100, 365}

// streakHistoryWindow caps how many recent completion dates the streak computation
// pulls — comfortably above the largest milestone so a 365-day streak still
// resolves, without scanning a user's whole history.
const streakHistoryWindow = 400

// SummaryNotifier gives every user (all plans, unified — NIC notification rework)
// an end-of-day evening_digest at their local evening hour: how many tasks they
// completed today and how many remain open. Fires whenever either is non-zero —
// even a 0-completed day still reports "N left" — and stays silent only when both
// are zero. streak_milestone remains a separate Pro-only celebration layered on
// top when the streak crosses a milestone.
//
// Streaks live here (not on the mutation path) because they depend on local day
// boundaries.
type SummaryNotifier struct {
	repo    Repository
	creator creator
	now     func() time.Time // injectable clock for tests
}

// NewSummaryNotifier builds the evening-digest sweep. Pass notification.Service as the creator.
func NewSummaryNotifier(repo Repository, c creator) *SummaryNotifier {
	return &SummaryNotifier{repo: repo, creator: c, now: time.Now}
}

// Run executes one sweep and returns a breakdown of what happened. Safe to re-run
// within the same local day: idempotency is guaranteed by each output's
// dedupe_key. When dryRun is true it computes the breakdown but inserts nothing.
func (n *SummaryNotifier) Run(ctx context.Context, dryRun bool) (*SweepBreakdown, error) {
	const sweep = "summary"
	users, err := n.repo.ListRemindableUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("jobs.SummaryNotifier: list users: %w", err)
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
		// This is the only sweep gated on the evening hour, not the morning hour.
		if !inFireWindow(local.Hour(), u.EveningHour) {
			b.skip(sweep, skipOutsideWindow, u.UserID, u.Timezone)
			continue
		}
		streaksActive := u.StreaksEnabled && u.Plan == planPro
		if !u.EveningDigestEnabled && !streaksActive {
			b.skip(sweep, skipToggleOff, u.UserID, u.Timezone)
			continue // user opted out of both this sweep's outputs
		}

		localToday := local.Format(scheduledForLayout)
		completed, err := n.repo.CountCompletedOn(ctx, u.UserID, u.Timezone, localToday)
		if err != nil {
			log.Error().Err(err).Str("user_id", u.UserID).Msg("summary sweep: count completed failed")
			continue
		}
		if dryRun {
			continue
		}

		if u.EveningDigestEnabled && n.emitDigest(ctx, u, localToday, completed) {
			b.Fired++
		}
		if streaksActive && n.emitStreak(ctx, u, localToday, completed) {
			b.Fired++
		}
	}
	return b, nil
}

// emitDigest creates the evening_digest when the user completed at least one task
// today, OR still has open tasks remaining — a 0-completed day with open work
// still gets "0 done today, N left". Silent only when both are zero. Returns
// whether a row was inserted.
func (n *SummaryNotifier) emitDigest(ctx context.Context, u RemindableUser, localToday string, completed int) bool {
	remaining, err := n.repo.CountOpenTasks(ctx, u.UserID)
	if err != nil {
		log.Error().Err(err).Str("user_id", u.UserID).Msg("summary sweep: count open tasks failed")
		return false
	}
	if completed == 0 && remaining == 0 {
		return false // nothing to report → stay silent
	}
	_, inserted, err := n.creator.Create(ctx, notification.Notification{
		UserID:    u.UserID,
		Type:      notification.TypeEveningDigest,
		Title:     "Your day, wrapped",
		Body:      eveningDigestBody(completed, remaining),
		Metadata:  eveningDigestMeta(completed, remaining),
		DedupeKey: notification.DedupeEveningDigest(u.UserID, localToday),
	})
	if err != nil {
		log.Error().Err(err).Str("user_id", u.UserID).Msg("summary sweep: digest create failed")
		return false
	}
	return inserted
}

// emitStreak creates a streak_milestone when today's completions carry the user's
// consecutive-day streak to a milestone value. No completion today ⇒ the streak
// doesn't include today ⇒ nothing fires. Returns whether a row was inserted.
func (n *SummaryNotifier) emitStreak(ctx context.Context, u RemindableUser, localToday string, completed int) bool {
	if completed == 0 {
		return false // streak must include today to be established today
	}

	dates, err := n.repo.RecentCompletionDates(ctx, u.UserID, u.Timezone, localToday, streakHistoryWindow)
	if err != nil {
		log.Error().Err(err).Str("user_id", u.UserID).Msg("summary sweep: streak dates failed")
		return false
	}
	streak := currentStreak(dates, localToday)
	if !isMilestone(streak) {
		return false
	}

	_, inserted, err := n.creator.Create(ctx, notification.Notification{
		UserID:    u.UserID,
		Type:      notification.TypeStreakMilestone,
		Title:     "Streak milestone!",
		Body:      fmt.Sprintf("You're on a %d-day completion streak. Keep it going!", streak),
		Metadata:  streakMeta(streak),
		DedupeKey: notification.DedupeStreakMilestone(u.UserID, streak),
	})
	if err != nil {
		log.Error().Err(err).Str("user_id", u.UserID).Msg("summary sweep: streak create failed")
		return false
	}
	return inserted
}

// currentStreak counts consecutive local days ending at localToday that each have a
// completion. dates is the distinct completion-date list (descending). If the most
// recent date isn't today, the streak doesn't include today and is 0.
func currentStreak(dates []string, localToday string) int {
	if len(dates) == 0 || dates[0] != localToday {
		return 0
	}
	day, err := time.Parse(scheduledForLayout, localToday)
	if err != nil {
		return 0
	}
	streak := 0
	for _, d := range dates {
		want := day.AddDate(0, 0, -streak).Format(scheduledForLayout)
		if d != want {
			break // gap → streak ends
		}
		streak++
	}
	return streak
}

// isMilestone reports whether a streak length is a celebrated milestone.
func isMilestone(streak int) bool {
	return slices.Contains(streakMilestones, streak)
}

// eveningDigestBody composes the human-readable wrap-up line, e.g.
// "You completed 3 tasks today. 4 tasks left."
func eveningDigestBody(completed, remaining int) string {
	body := fmt.Sprintf("You completed %d %s today.", completed, plural(completed, "task", "tasks"))
	if remaining > 0 {
		body += fmt.Sprintf(" %d %s left.", remaining, plural(remaining, "task", "tasks"))
	}
	return body
}

// eveningDigestMeta encodes the completed + remaining counts for the client.
func eveningDigestMeta(completed, remaining int) json.RawMessage {
	b, _ := json.Marshal(struct {
		Completed int `json:"completed"`
		Remaining int `json:"remaining"`
	}{Completed: completed, Remaining: remaining})
	return b
}

// streakMeta encodes the streak length for the client.
func streakMeta(streak int) json.RawMessage {
	b, _ := json.Marshal(struct {
		Streak int `json:"streak"`
	}{Streak: streak})
	return b
}
