// Package recurrence owns task recurrence (E-050): the rule template, its
// schedule, and the cursor that says which date comes next.
//
// The schedule is a deliberate subset of RFC 5545 stored as columns rather than
// an RRULE string, so the frontend can render "every 2 weeks on Mon, Thu" without
// shipping a parser.
package recurrence

import "time"

// Frequencies. A small closed set — anything outside it is rejected at validation.
const (
	FreqDaily   = "daily"
	FreqWeekly  = "weekly"
	FreqMonthly = "monthly"
	FreqYearly  = "yearly"
)

// MonthdayLast selects the last day of the month, whatever its length.
const MonthdayLast = -1

// dateLayout is the wire/date format. Recurrence has no time-of-day: tasks carry
// a YYYY-MM-DD scheduled_for and due_date was dropped in migration 026.
const dateLayout = "2006-01-02"

// maxSearchIterations bounds the weekly/monthly scan so a rule can never spin.
// Weekly needs at most 7 steps past the interval jump; monthly at most 12 for a
// yearly-ish stride. 400 is far beyond any legitimate case and turns a would-be
// infinite loop into "no next occurrence".
const maxSearchIterations = 400

// NextOccurrence returns the first occurrence strictly after `after`, or the
// series start if that is later. Pure: no clock, no DB — the caller injects
// every date.
//
// Returns ok=false when the series is exhausted (the next date would fall past
// end_date), which is the caller's signal to null the cursor.
func NextOccurrence(r Rule, after time.Time) (time.Time, bool) {
	start := dayOf(r.StartDate)
	from := dayOf(after)

	// Before the series begins, the first candidate is the start date itself —
	// but it still has to satisfy the weekday/monthday constraint.
	var candidate time.Time
	if from.Before(start) {
		candidate = start
	} else {
		candidate = from.AddDate(0, 0, 1)
	}

	next, ok := seek(r, start, candidate)
	if !ok {
		return time.Time{}, false
	}
	if r.EndDate != nil && next.After(dayOf(*r.EndDate)) {
		return time.Time{}, false
	}
	return next, true
}

// seek finds the first date >= candidate that lands on the rule's schedule.
func seek(r Rule, start, candidate time.Time) (time.Time, bool) {
	interval := r.Interval
	if interval < 1 {
		interval = 1
	}

	switch r.Freq {
	case FreqDaily:
		// Snap forward onto the interval lattice anchored at start.
		elapsed := daysBetween(start, candidate)
		if rem := elapsed % interval; rem != 0 {
			candidate = candidate.AddDate(0, 0, interval-rem)
		}
		return candidate, true

	case FreqWeekly:
		return seekWeekly(r, start, candidate, interval)

	case FreqMonthly:
		return seekMonthly(r, start, candidate, interval)

	case FreqYearly:
		// Same month/day as the start, every `interval` years. Feb 29 clamps to
		// Feb 28 in a common year rather than skipping the anniversary.
		year := candidate.Year()
		if elapsed := year - start.Year(); elapsed%interval != 0 {
			year += interval - mod(elapsed, interval)
		}
		for i := 0; i < maxSearchIterations; i++ {
			d := clampDay(year, start.Month(), start.Day())
			if !d.Before(candidate) {
				return d, true
			}
			year += interval
		}
	}
	return time.Time{}, false
}

// seekWeekly walks day by day, accepting a date only when it falls on a selected
// weekday inside an on-interval week. An empty ByWeekday falls back to the start
// date's weekday.
func seekWeekly(r Rule, start, candidate time.Time, interval int) (time.Time, bool) {
	days := r.ByWeekday
	if len(days) == 0 {
		days = []int{int(start.Weekday())}
	}
	selected := make(map[time.Weekday]bool, len(days))
	for _, d := range days {
		selected[time.Weekday(d)] = true
	}

	// Weeks are measured from the Sunday of the start week so the interval is
	// stable regardless of which weekday the search happens to land on.
	startWeek := weekStart(start)

	for i := 0; i < maxSearchIterations; i++ {
		if selected[candidate.Weekday()] {
			weeks := daysBetween(startWeek, weekStart(candidate)) / 7
			if weeks%interval == 0 {
				return candidate, true
			}
			// Off-interval week: jump to the start of the next on-interval week
			// instead of crawling a day at a time.
			skip := interval - mod(weeks, interval)
			candidate = weekStart(candidate).AddDate(0, 0, 7*skip)
			continue
		}
		candidate = candidate.AddDate(0, 0, 1)
	}
	return time.Time{}, false
}

// seekMonthly walks month by month. The target day clamps to the month's length,
// so "the 31st" yields the 30th in a 30-day month and the 28th/29th in February.
// A monthly obligation must never vanish because the month is short.
func seekMonthly(r Rule, start, candidate time.Time, interval int) (time.Time, bool) {
	day := start.Day()
	if r.ByMonthday != nil {
		day = *r.ByMonthday
	}

	// Align the cursor month onto the interval lattice anchored at start.
	year, month := candidate.Year(), candidate.Month()
	elapsed := monthsBetween(start, candidate)
	if rem := mod(elapsed, interval); rem != 0 {
		year, month = addMonths(year, month, interval-rem)
	}

	for i := 0; i < maxSearchIterations; i++ {
		d := clampDay(year, month, day)
		if !d.Before(candidate) {
			return d, true
		}
		year, month = addMonths(year, month, interval)
	}
	return time.Time{}, false
}

// clampDay builds a date, clamping the day to the month's real length. day == -1
// (MonthdayLast) means the last day of that month.
func clampDay(year int, month time.Month, day int) time.Time {
	last := daysInMonth(year, month)
	if day == MonthdayLast || day > last {
		day = last
	}
	if day < 1 {
		day = 1
	}
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func daysInMonth(year int, month time.Month) int {
	// Day 0 of the following month is the last day of this one.
	return time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func addMonths(year int, month time.Month, n int) (int, time.Month) {
	total := int(month) - 1 + n
	return year + floorDiv(total, 12), time.Month(mod(total, 12) + 1)
}

// dayOf strips any time-of-day and normalises to UTC midnight, so arithmetic is
// never perturbed by a DST transition in the caller's location.
func dayOf(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// weekStart is the Sunday on or before t.
func weekStart(t time.Time) time.Time {
	return t.AddDate(0, 0, -int(t.Weekday()))
}

// daysBetween counts whole days from a to b. Both are UTC midnights, so the
// division is exact and DST-proof.
func daysBetween(a, b time.Time) int {
	return int(b.Sub(a).Hours() / 24)
}

func monthsBetween(a, b time.Time) int {
	return (b.Year()-a.Year())*12 + int(b.Month()) - int(a.Month())
}

// mod is a Euclidean modulo — always non-negative, unlike Go's %.
func mod(a, b int) int {
	m := a % b
	if m < 0 {
		m += b
	}
	return m
}

func floorDiv(a, b int) int {
	q := a / b
	if a%b < 0 {
		q--
	}
	return q
}

// FormatDate renders a date in the wire format.
func FormatDate(t time.Time) string { return t.Format(dateLayout) }

// ParseDate parses a YYYY-MM-DD wire date at UTC midnight.
func ParseDate(s string) (time.Time, error) { return time.Parse(dateLayout, s) }
