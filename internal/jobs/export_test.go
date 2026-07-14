package jobs

import "time"

// SetClock overrides the notifier's clock. Test-only seam (integration tests live
// in package jobs_test, so they can't touch the unexported field directly).
func SetClock(n *DueDateNotifier, clock func() time.Time) {
	n.now = clock
}
