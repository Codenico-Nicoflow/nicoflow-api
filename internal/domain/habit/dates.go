package habit

import (
	"fmt"
	"slices"
	"time"
)

// Date arithmetic for habits. Everything here is pure and zone-explicit: no
// function reads the machine's local zone, because the ambient answer is right
// in a UTC container and wrong for a real user in Auckland — which is how a
// timezone bug ships green.

// localDate converts an instant to the calendar date it falls on in tz.
//
// cutoffHour shifts the day boundary later: with cutoff 3, a check-in at 01:00
// still counts for the previous day. It ships at 0 (midnight) and exists so the
// "my day ends at 3am" setting is later a toggle rather than a migration over
// live check-in data.
func localDate(instant time.Time, loc *time.Location, cutoffHour int) time.Time {
	local := instant.In(loc)
	if cutoffHour > 0 && local.Hour() < cutoffHour {
		local = local.AddDate(0, 0, -1)
	}
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

// loadLocation resolves an IANA zone, falling back to UTC. A stored zone that no
// longer resolves must not make a habit un-checkable: the user would be locked
// out of their own streak by a tzdata change they never made.
func loadLocation(tz string) *time.Location {
	if tz == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

// parseDate reads a wire date (YYYY-MM-DD) as a zone-independent calendar day.
// Dates are days, not instants, so they are anchored at UTC midnight and never
// carry a clock time that could shift them across a boundary.
func parseDate(s string) (time.Time, error) {
	d, err := time.ParseInLocation(DateLayout, s, time.UTC)
	if err != nil {
		return time.Time{}, invalid("date must be formatted YYYY-MM-DD")
	}
	return d, nil
}

// DefaultWeekStart is Monday, matching the users.week_start column default.
const DefaultWeekStart = 1

// weekStart returns the first day of the week containing d, where firstDay is a
// weekday index (0=Sunday … 6=Saturday) taken from users.week_start.
//
// It follows the user's own setting rather than a fixed Monday because the work
// week is Mon–Fri across most of Europe but Sun–Thu in Israel, and the product
// ships Hebrew. A Sunday-start user whose habit weeks silently began on Monday
// would see "3× this week" straddle what they consider two different weeks.
//
// The cost is real and worth stating: this boundary decides which check-ins
// count toward a quota, so changing the setting redraws historical weeks and a
// quota streak can shift. Freezing the boundary onto each habit at creation —
// the trick target_at_checkin already uses — is the fix if that becomes a
// problem in practice.
// firstDay is a POINTER because Sunday is 0 and so is the zero value: a habit
// nobody stamped a preference onto must fall back to Monday rather than
// silently becoming a Sunday-start habit.
func weekStart(d time.Time, firstDay *int) time.Time {
	start := DefaultWeekStart
	if firstDay != nil && *firstDay >= 0 && *firstDay <= 6 {
		start = *firstDay
	}
	offset := (int(d.Weekday()) - start + 7) % 7
	return d.AddDate(0, 0, -offset)
}

// IsScheduledOn reports whether a habit is due on a given date.
//
// Quota habits are due every day until the week's quota is met, so this reports
// true for them and the caller applies the quota rule; there is no such thing as
// an unscheduled day for a "3 times a week" habit.
func IsScheduledOn(h Habit, d time.Time) bool {
	if h.ScheduleKind != ScheduleWeekdays {
		return true
	}

	wd := d.Weekday()

	return slices.ContainsFunc(h.ByWeekday, func(day int16) bool {
		return time.Weekday(day) == wd
	})
}

// satisfies applies the polarity rule. A build habit clears its target from
// below; a quit habit stays at or under it — which is why a quit habit's target
// is normally 0 and logging a slip fails the day.
func satisfies(polarity string, value, target int) bool {
	if polarity == PolarityQuit {
		return value <= target
	}
	return value >= target
}

// validateCheckInDate bounds a backfill. A future date is always refused; a past
// date must fall inside the habit's window.
//
// The window differs by schedule kind because the unit differs: day habits are
// corrected day-by-day, while a quota habit's week is the thing that succeeds or
// fails, so its window is expressed in weeks and a closed week locks.
func validateCheckInDate(h Habit, date, today time.Time) error {
	if date.After(today) {
		return invalid("cannot check in for a future date")
	}

	if h.ScheduleKind == ScheduleWeeklyQuota {
		earliest := weekStart(today, h.WeekStart).AddDate(0, 0, -7*BackfillWeeks)
		if date.Before(earliest) {
			return invalid(fmt.Sprintf(
				"cannot check in earlier than the previous week (from %s)", earliest.Format(DateLayout)))
		}
		return nil
	}

	earliest := today.AddDate(0, 0, -BackfillDays)
	if date.Before(earliest) {
		return invalid(fmt.Sprintf(
			"cannot check in more than %d days ago (from %s)", BackfillDays, earliest.Format(DateLayout)))
	}
	return nil
}
