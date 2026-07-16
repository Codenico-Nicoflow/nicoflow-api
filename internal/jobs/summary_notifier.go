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

// summaryLocalHour is the local hour (24h) at which the end-of-day wrap fires.
const summaryLocalHour = 20

// streakMilestones are the consecutive-day streak lengths worth celebrating. A
// streak_milestone fires when the current streak exactly equals one of these; the
// per-milestone dedupe key makes it a once-ever event.
var streakMilestones = []int{7, 30, 100, 365}

// streakHistoryWindow caps how many recent completion dates the streak computation
// pulls — comfortably above the largest milestone so a 365-day streak still
// resolves, without scanning a user's whole history.
const streakHistoryWindow = 400

// SummaryNotifier gives Pro users an end-of-day wrap at their local summaryLocalHour:
//   - daily_summary (Pro): count of tasks completed today. Dedupe per local day.
//   - streak_milestone (Pro): when the consecutive-day completion streak reaches a
//     milestone. Dedupe per milestone (once ever).
//
// Streaks live here (not on the mutation path) because they depend on local day
// boundaries. Both types are Pro-only; free users are skipped.
type SummaryNotifier struct {
	repo    Repository
	creator creator
	now     func() time.Time // injectable clock for tests
}

// NewSummaryNotifier builds the end-of-day sweep. Pass notification.Service as the creator.
func NewSummaryNotifier(repo Repository, c creator) *SummaryNotifier {
	return &SummaryNotifier{repo: repo, creator: c, now: time.Now}
}

// Run executes one sweep and returns how many notifications were newly created
// (dedupe-suppressed duplicates not counted). Safe to re-run within the same local
// day: idempotency is guaranteed by each output's dedupe_key.
func (n *SummaryNotifier) Run(ctx context.Context) (int, error) {
	users, err := n.repo.ListRemindableUsers(ctx)
	if err != nil {
		return 0, fmt.Errorf("jobs.SummaryNotifier: list users: %w", err)
	}

	nowUTC := n.now().UTC()
	generated := 0

	for _, u := range users {
		localToday, ok := summaryLocalToday(nowUTC, u.Timezone)
		if !ok {
			continue // not this user's end-of-day hour (or a bad timezone) → skip
		}
		if !notification.IsProType(notification.TypeDailySummary) || u.Plan != planPro {
			continue // both outputs are Pro-only
		}
		if !u.DailySummaryEnabled && !u.StreaksEnabled {
			continue // user opted out of both this sweep's families
		}

		completed, err := n.repo.CountCompletedOn(ctx, u.UserID, u.Timezone, localToday)
		if err != nil {
			log.Error().Err(err).Str("user_id", u.UserID).Msg("summary sweep: count completed failed")
			continue
		}

		if u.DailySummaryEnabled && n.emitSummary(ctx, u, localToday, completed) {
			generated++
		}
		if u.StreaksEnabled && n.emitStreak(ctx, u, localToday, completed) {
			generated++
		}
	}
	return generated, nil
}

// emitSummary creates the daily_summary when the user completed at least one task
// today. Returns whether a row was inserted.
func (n *SummaryNotifier) emitSummary(ctx context.Context, u RemindableUser, localToday string, completed int) bool {
	if completed == 0 {
		return false // nothing done → no wrap
	}
	_, inserted, err := n.creator.Create(ctx, notification.Notification{
		UserID:    u.UserID,
		Type:      notification.TypeDailySummary,
		Title:     "Your day, wrapped",
		Body:      fmt.Sprintf("You completed %d %s today.", completed, plural(completed, "task", "tasks")),
		Metadata:  completedCountMeta(completed),
		DedupeKey: notification.DedupeDailySummary(u.UserID, localToday),
	})
	if err != nil {
		log.Error().Err(err).Str("user_id", u.UserID).Msg("summary sweep: summary create failed")
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

// summaryLocalToday reports whether the user should be swept this hour and, if so,
// their local ISO "today". A user fires only when their local hour equals
// summaryLocalHour; an unparseable timezone is skipped (ok=false).
func summaryLocalToday(nowUTC time.Time, tz string) (string, bool) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return "", false
	}
	local := nowUTC.In(loc)
	if local.Hour() != summaryLocalHour {
		return "", false
	}
	return local.Format(scheduledForLayout), true
}

// completedCountMeta encodes the completed-today count for the client.
func completedCountMeta(count int) json.RawMessage {
	b, _ := json.Marshal(struct {
		Count int `json:"count"`
	}{Count: count})
	return b
}

// streakMeta encodes the streak length for the client.
func streakMeta(streak int) json.RawMessage {
	b, _ := json.Marshal(struct {
		Streak int `json:"streak"`
	}{Streak: streak})
	return b
}
