package recurrence

// CurrentStreakForTest exposes the streak walk to the integration suite, which
// lives in package recurrence_test and can't reach the unexported function.
func CurrentStreakForTest(statuses []string) int { return currentStreak(statuses) }
