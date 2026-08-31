package jobs

import "time"

// SetDayStartClock overrides the day-start (morning digest) notifier's clock.
// Test-only seam.
func SetDayStartClock(n *DayStartNotifier, clock func() time.Time) {
	n.now = clock
}

// SetSummaryClock overrides the summary (evening digest) notifier's clock.
// Test-only seam.
func SetSummaryClock(n *SummaryNotifier, clock func() time.Time) {
	n.now = clock
}
