package jobs

import (
	"time"

	"github.com/rs/zerolog/log"
)

// catchUpHours is how many hourly ticks after a user's reminder hour the sweep
// will still fire. Insures against a missed tick (DST spring-forward, a failed
// cron run, a cold start) without firing so late the notification is useless.
const catchUpHours = 3

// inFireWindow reports whether the current local hour falls within a user's
// reminder window: their reminder hour plus up to catchUpHours-1 catch-up ticks.
//
// The end is clamped to 24 (mandatory, not a preference): Hour() wraps to 0 at
// midnight, so an unclamped `hour < 22+3` would evaluate `hour < 25` — trivially
// true at 01:00 and every hour after, leaving the window open all night. The
// window never wraps past midnight: dedupe keys are local-date based, so a 00:00
// catch-up for yesterday's hour would be written under today's key and suppress
// tonight's real notification.
func inFireWindow(hour, reminderHour int) bool {
	end := min(reminderHour+catchUpHours, 24)
	return hour >= reminderHour && hour < end
}

// skipReason enumerates why a user was not fired in a sweep. Values double as the
// JSON keys in SweepBreakdown.Skipped and the structured-log reason.
const (
	skipOutsideWindow = "outside_window"
	skipBadTimezone   = "bad_timezone"
	skipPlanGate      = "plan_gate"
	skipToggleOff     = "toggle_off"
)

// SweepBreakdown replaces the bare {"generated":N} sweep response. It turns "why
// did nothing fire" from a live-DB investigation into one curl: considered is
// every user examined, fired is how many notifications were newly inserted, and
// skipped buckets the rest by reason.
type SweepBreakdown struct {
	Considered int            `json:"considered"`
	Fired      int            `json:"fired"`
	Skipped    map[string]int `json:"skipped"`
}

// newBreakdown returns a breakdown with every skip bucket present at 0, so the
// JSON shape is stable regardless of which reasons occurred this run.
func newBreakdown() *SweepBreakdown {
	return &SweepBreakdown{
		Skipped: map[string]int{
			skipOutsideWindow: 0,
			skipBadTimezone:   0,
			skipPlanGate:      0,
			skipToggleOff:     0,
		},
	}
}

// skip records a skip against its reason bucket and logs at the reason's severity:
// outside_window is normal (~23/24 of users every hour) and stays at debug;
// bad_timezone is a data-integrity bug and logs at warn with the user + offending
// string so it surfaces without a live probe.
func (b *SweepBreakdown) skip(sweep, reason, userID, tz string) {
	b.Skipped[reason]++
	switch reason {
	case skipBadTimezone:
		log.Warn().Str("sweep", sweep).Str("user_id", userID).Str("timezone", tz).
			Msg("sweep skipped: timezone failed to load")
	default:
		log.Debug().Str("sweep", sweep).Str("user_id", userID).Str("reason", reason).
			Msg("sweep skipped")
	}
}

// localHour resolves a user's current local hour, reporting ok=false when the
// timezone can't be loaded (a data-integrity fault the caller records as
// skipBadTimezone). Kept separate from inFireWindow so the caller distinguishes a
// bad zone from a merely-out-of-window user.
func localHour(nowUTC time.Time, tz string) (time.Time, bool) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Time{}, false
	}
	return nowUTC.In(loc), true
}
