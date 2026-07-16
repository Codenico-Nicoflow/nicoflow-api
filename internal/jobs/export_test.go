package jobs

import (
	"time"

	"github.com/nicoflow/nicoflow-api/pkg/emailutil"
)

// SetClock overrides the notifier's clock. Test-only seam (integration tests live
// in package jobs_test, so they can't touch the unexported field directly).
func SetClock(n *DueDateNotifier, clock func() time.Time) {
	n.now = clock
}

// SetDigestSender overrides the digest email sender. Test-only seam.
func SetDigestSender(n *DueDateNotifier, s func(to string, tasks []emailutil.DigestTask, smtpDSN string) error) {
	n.sendDigest = s
}

// SetOverdueClock overrides the overdue notifier's clock. Test-only seam.
func SetOverdueClock(n *OverdueNotifier, clock func() time.Time) {
	n.now = clock
}

// SetDayStartClock overrides the day-start notifier's clock. Test-only seam.
func SetDayStartClock(n *DayStartNotifier, clock func() time.Time) {
	n.now = clock
}
