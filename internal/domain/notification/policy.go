package notification

import "fmt"

// proTypes is the single source of truth for which notification types are Pro-only.
// Producers gate on IsProType — none classify a type themselves. Everything not
// listed here is a free type delivered to all plans.
var proTypes = map[string]struct{}{
	TypeNothingScheduled: {},
	TypeInboxUnprocessed: {},
	TypeInboxStale:       {},
	TypeDailySummary:     {},
	TypeInboxZero:        {},
	TypeStreakMilestone:  {},
}

// IsProType reports whether a notification type is Pro-only. Free users never
// receive Pro types (the producer skips them); free types deliver to all plans.
func IsProType(t string) bool {
	_, ok := proTypes[t]
	return ok
}

// Dedupe-key builders. Each key is scoped to a natural time bucket (day/week/
// milestone) so re-running a producer within that window collides on the
// (user_id, dedupe_key) unique index → ON CONFLICT DO NOTHING. Never key on a
// wall-clock instant. All return *string to match Notification.DedupeKey.

func ptr(s string) *string { return &s }

// DedupeTaskOverdue: one nag per task per local day.
func DedupeTaskOverdue(taskID, localDate string) *string {
	return ptr(fmt.Sprintf("%s:%s:%s", TypeTaskOverdue, taskID, localDate))
}

// DedupeTaskScheduledToday: one summary per user per local day.
func DedupeTaskScheduledToday(userID, localDate string) *string {
	return ptr(fmt.Sprintf("%s:%s:%s", TypeTaskScheduledToday, userID, localDate))
}

// DedupeDayPlanNudge: one nudge per user per local day.
func DedupeDayPlanNudge(userID, localDate string) *string {
	return ptr(fmt.Sprintf("%s:%s:%s", TypeNothingScheduled, userID, localDate))
}

// DedupeInboxUnprocessed: one reminder per user per local day.
func DedupeInboxUnprocessed(userID, localDate string) *string {
	return ptr(fmt.Sprintf("%s:%s:%s", TypeInboxUnprocessed, userID, localDate))
}

// DedupeInboxStale: one warning per user per ISO week (don't nag daily).
func DedupeInboxStale(userID, isoWeek string) *string {
	return ptr(fmt.Sprintf("%s:%s:%s", TypeInboxStale, userID, isoWeek))
}

// DedupeTaskCompleted: once per task (re-completing after reopen is suppressed).
func DedupeTaskCompleted(taskID string) *string {
	return ptr(fmt.Sprintf("%s:%s", TypeTaskCompleted, taskID))
}

// DedupeProjectCompleted: once per project per local day.
func DedupeProjectCompleted(projectID, localDate string) *string {
	return ptr(fmt.Sprintf("%s:%s:%s", TypeProjectCompleted, projectID, localDate))
}

// DedupeDailySummary: one summary per user per local day.
func DedupeDailySummary(userID, localDate string) *string {
	return ptr(fmt.Sprintf("%s:%s:%s", TypeDailySummary, userID, localDate))
}

// DedupeInboxZero: once per user per local day (reaching zero repeatedly won't spam).
func DedupeInboxZero(userID, localDate string) *string {
	return ptr(fmt.Sprintf("%s:%s:%s", TypeInboxZero, userID, localDate))
}

// DedupeStreakMilestone: once per user per milestone value, ever.
func DedupeStreakMilestone(userID string, milestone int) *string {
	return ptr(fmt.Sprintf("%s:%s:%d", TypeStreakMilestone, userID, milestone))
}
