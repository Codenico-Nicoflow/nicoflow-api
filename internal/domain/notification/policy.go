package notification

import "fmt"

// proTypes is the single source of truth for which notification types are Pro-only.
// Producers gate on IsProType — none classify a type themselves. Everything not
// listed here is a free type delivered to all plans. MorningDigest/EveningDigest
// are deliberately absent — the notification rework unified both daily
// touchpoints across plans.
var proTypes = map[string]struct{}{
	TypeInboxZero:       {},
	TypeStreakMilestone: {},
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

// DedupeTaskCompleted: once per task (re-completing after reopen is suppressed).
func DedupeTaskCompleted(taskID string) *string {
	return ptr(fmt.Sprintf("%s:%s", TypeTaskCompleted, taskID))
}

// DedupeProjectCompleted: scoped to one explicit status→completed transition
// (the caller passes a value unique to that transition, e.g. an update
// timestamp) — reopening and re-completing later gets its own key and fires
// again.
func DedupeProjectCompleted(projectID, transitionKey string) *string {
	return ptr(fmt.Sprintf("%s:%s:%s", TypeProjectCompleted, projectID, transitionKey))
}

// DedupeInboxZero: once per user per local day (reaching zero repeatedly won't spam).
func DedupeInboxZero(userID, localDate string) *string {
	return ptr(fmt.Sprintf("%s:%s:%s", TypeInboxZero, userID, localDate))
}

// DedupeStreakMilestone: once per user per milestone value, ever.
func DedupeStreakMilestone(userID string, milestone int) *string {
	return ptr(fmt.Sprintf("%s:%s:%d", TypeStreakMilestone, userID, milestone))
}

// DedupeMorningDigest: one digest per user per local day.
func DedupeMorningDigest(userID, localDate string) *string {
	return ptr(fmt.Sprintf("%s:%s:%s", TypeMorningDigest, userID, localDate))
}

// DedupeEveningDigest: one digest per user per local day.
func DedupeEveningDigest(userID, localDate string) *string {
	return ptr(fmt.Sprintf("%s:%s:%s", TypeEveningDigest, userID, localDate))
}
