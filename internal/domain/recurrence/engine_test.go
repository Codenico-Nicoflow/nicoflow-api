package recurrence

import (
	"testing"
	"time"
)

func date(s string) time.Time {
	d, err := ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

func ptrTime(s string) *time.Time { d := date(s); return &d }
func ptrInt(i int) *int           { return &i }

func TestNextOccurrence(t *testing.T) {
	tests := []struct {
		name  string
		rule  Rule
		after string
		want  string // empty = expect ok == false
	}{
		// Daily
		{"daily every day", Rule{Freq: FreqDaily, Interval: 1, StartDate: date("2026-03-01")}, "2026-03-01", "2026-03-02"},
		{"daily interval 3 on lattice", Rule{Freq: FreqDaily, Interval: 3, StartDate: date("2026-03-01")}, "2026-03-01", "2026-03-04"},
		{"daily interval 3 snaps forward off lattice", Rule{Freq: FreqDaily, Interval: 3, StartDate: date("2026-03-01")}, "2026-03-02", "2026-03-04"},
		{"daily before start yields start", Rule{Freq: FreqDaily, Interval: 1, StartDate: date("2026-03-10")}, "2026-03-01", "2026-03-10"},
		{"daily crosses month boundary", Rule{Freq: FreqDaily, Interval: 1, StartDate: date("2026-03-31")}, "2026-03-31", "2026-04-01"},
		{"daily crosses year boundary", Rule{Freq: FreqDaily, Interval: 1, StartDate: date("2026-12-31")}, "2026-12-31", "2027-01-01"},

		// Weekly. 2026-03-02 is a Monday.
		{"weekly falls back to start weekday", Rule{Freq: FreqWeekly, Interval: 1, StartDate: date("2026-03-02")}, "2026-03-02", "2026-03-09"},
		{"weekly multi-weekday picks nearer day", Rule{Freq: FreqWeekly, Interval: 1, ByWeekday: []int{1, 4}, StartDate: date("2026-03-02")}, "2026-03-02", "2026-03-05"},
		{"weekly multi-weekday wraps to next week", Rule{Freq: FreqWeekly, Interval: 1, ByWeekday: []int{1, 4}, StartDate: date("2026-03-02")}, "2026-03-05", "2026-03-09"},
		{"weekly interval 2 skips the off week", Rule{Freq: FreqWeekly, Interval: 2, ByWeekday: []int{1}, StartDate: date("2026-03-02")}, "2026-03-02", "2026-03-16"},
		{"weekly interval 2 multi-weekday stays in week", Rule{Freq: FreqWeekly, Interval: 2, ByWeekday: []int{1, 4}, StartDate: date("2026-03-02")}, "2026-03-02", "2026-03-05"},

		// Monthly + clamping
		{"monthly same day", Rule{Freq: FreqMonthly, Interval: 1, StartDate: date("2026-03-15")}, "2026-03-15", "2026-04-15"},
		{"monthly 31st clamps to 30-day month", Rule{Freq: FreqMonthly, Interval: 1, ByMonthday: ptrInt(31), StartDate: date("2026-01-31")}, "2026-03-31", "2026-04-30"},
		{"monthly 31st clamps to February common year", Rule{Freq: FreqMonthly, Interval: 1, ByMonthday: ptrInt(31), StartDate: date("2026-01-31")}, "2026-01-31", "2026-02-28"},
		{"monthly 31st clamps to February leap year", Rule{Freq: FreqMonthly, Interval: 1, ByMonthday: ptrInt(31), StartDate: date("2028-01-31")}, "2028-01-31", "2028-02-29"},
		{"monthly last day of month", Rule{Freq: FreqMonthly, Interval: 1, ByMonthday: ptrInt(MonthdayLast), StartDate: date("2026-01-31")}, "2026-01-31", "2026-02-28"},
		{"monthly interval 3", Rule{Freq: FreqMonthly, Interval: 3, ByMonthday: ptrInt(10), StartDate: date("2026-01-10")}, "2026-01-10", "2026-04-10"},
		{"monthly crosses year boundary", Rule{Freq: FreqMonthly, Interval: 1, ByMonthday: ptrInt(15), StartDate: date("2026-12-15")}, "2026-12-15", "2027-01-15"},

		// Yearly
		{"yearly", Rule{Freq: FreqYearly, Interval: 1, StartDate: date("2026-06-01")}, "2026-06-01", "2027-06-01"},
		{"yearly interval 2", Rule{Freq: FreqYearly, Interval: 2, StartDate: date("2026-06-01")}, "2026-06-01", "2028-06-01"},
		{"yearly Feb 29 clamps in a common year", Rule{Freq: FreqYearly, Interval: 1, StartDate: date("2028-02-29")}, "2028-02-29", "2029-02-28"},

		// end_date exhaustion
		{"end date exhausted returns no occurrence", Rule{Freq: FreqDaily, Interval: 1, StartDate: date("2026-03-01"), EndDate: ptrTime("2026-03-02")}, "2026-03-02", ""},
		{"end date on the boundary is inclusive", Rule{Freq: FreqDaily, Interval: 1, StartDate: date("2026-03-01"), EndDate: ptrTime("2026-03-02")}, "2026-03-01", "2026-03-02"},

		// A DST-shifting date must not perturb pure date math.
		{"daily across a DST spring-forward", Rule{Freq: FreqDaily, Interval: 1, StartDate: date("2026-03-28")}, "2026-03-28", "2026-03-29"},
		{"daily across a DST fall-back", Rule{Freq: FreqDaily, Interval: 1, StartDate: date("2026-10-24")}, "2026-10-24", "2026-10-25"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NextOccurrence(tt.rule, date(tt.after))
			if tt.want == "" {
				if ok {
					t.Fatalf("ok = true (%s), want no occurrence", FormatDate(got))
				}
				return
			}
			if !ok {
				t.Fatalf("ok = false, want %s", tt.want)
			}
			if FormatDate(got) != tt.want {
				t.Errorf("next = %s, want %s", FormatDate(got), tt.want)
			}
		})
	}
}

// A rule's own start date is eligible when the caller seeks from the day before,
// which is how the service materializes instance #1.
func TestNextOccurrence_StartDateIsEligible(t *testing.T) {
	tests := []struct {
		name string
		rule Rule
		want string
	}{
		{"daily", Rule{Freq: FreqDaily, Interval: 1, StartDate: date("2026-03-02")}, "2026-03-02"},
		{"weekly on the start weekday", Rule{Freq: FreqWeekly, Interval: 1, ByWeekday: []int{1}, StartDate: date("2026-03-02")}, "2026-03-02"},
		{"monthly on the start monthday", Rule{Freq: FreqMonthly, Interval: 1, ByMonthday: ptrInt(2), StartDate: date("2026-03-02")}, "2026-03-02"},
		{"yearly", Rule{Freq: FreqYearly, Interval: 1, StartDate: date("2026-03-02")}, "2026-03-02"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := NextOccurrence(tt.rule, tt.rule.StartDate.AddDate(0, 0, -1))
			if !ok || FormatDate(got) != tt.want {
				t.Errorf("first = %s (ok=%v), want %s", FormatDate(got), ok, tt.want)
			}
		})
	}
}

// A weekly rule whose start weekday is not in ByWeekday must roll forward to the
// first selected day rather than firing on the start date.
func TestNextOccurrence_WeeklyStartNotInSelection(t *testing.T) {
	// 2026-03-02 is a Monday; select Thursday only.
	r := Rule{Freq: FreqWeekly, Interval: 1, ByWeekday: []int{4}, StartDate: date("2026-03-02")}
	got, ok := NextOccurrence(r, r.StartDate.AddDate(0, 0, -1))
	if !ok || FormatDate(got) != "2026-03-05" {
		t.Errorf("first = %s (ok=%v), want 2026-03-05", FormatDate(got), ok)
	}
}

// The engine reads only the injected date — a time-of-day on the input must not
// change the result.
func TestNextOccurrence_IgnoresTimeOfDay(t *testing.T) {
	r := Rule{Freq: FreqDaily, Interval: 1, StartDate: date("2026-03-01")}
	withTime := time.Date(2026, 3, 1, 23, 59, 59, 0, time.FixedZone("late", 14*3600))
	got, ok := NextOccurrence(r, withTime)
	if !ok || FormatDate(got) != "2026-03-02" {
		t.Errorf("next = %s (ok=%v), want 2026-03-02", FormatDate(got), ok)
	}
}

func TestClampDay(t *testing.T) {
	tests := []struct {
		name      string
		year, day int
		month     time.Month
		want      string
	}{
		{"exact day", 2026, 15, time.March, "2026-03-15"},
		{"31 in a 30-day month", 2026, 31, time.April, "2026-04-30"},
		{"31 in February common", 2026, 31, time.February, "2026-02-28"},
		{"31 in February leap", 2028, 31, time.February, "2028-02-29"},
		{"last day sentinel", 2026, MonthdayLast, time.April, "2026-04-30"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatDate(clampDay(tt.year, tt.month, tt.day)); got != tt.want {
				t.Errorf("clampDay = %s, want %s", got, tt.want)
			}
		})
	}
}
