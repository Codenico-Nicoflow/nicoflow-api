package habit

import (
	"testing"
	"time"
)

// These tests pin an explicit instant and an explicit zone. The whole point is
// that the ambient answer — the one a UTC CI container gives — is wrong for a
// real user, so nothing here may read the machine's local zone.

func mustLoad(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("LoadLocation(%q): %v", name, err)
	}
	return loc
}

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func TestLocalDate(t *testing.T) {
	tests := []struct {
		name    string
		instant time.Time
		zone    string
		cutoff  int
		want    time.Time
	}{
		{
			// The headline case: 09:00 in Auckland is still the previous day in
			// UTC. Storing the UTC date would credit the check-in to yesterday.
			name:    "ahead of UTC, morning",
			instant: time.Date(2026, 8, 4, 21, 0, 0, 0, time.UTC), // 09:00 Aug 5 NZ
			zone:    "Pacific/Auckland",
			want:    date(2026, time.August, 5),
		},
		{
			// The mirror case: late evening in Los Angeles is already tomorrow
			// in UTC.
			name:    "behind UTC, late evening",
			instant: time.Date(2026, 8, 6, 6, 0, 0, 0, time.UTC), // 23:00 Aug 5 LA
			zone:    "America/Los_Angeles",
			want:    date(2026, time.August, 5),
		},
		{
			name:    "utc noon",
			instant: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC),
			zone:    "UTC",
			want:    date(2026, time.August, 5),
		},
		{
			// A night owl checking in at 01:00 with a 3am cutoff is still
			// finishing the previous day.
			name:    "before the cutoff counts as yesterday",
			instant: time.Date(2026, 8, 5, 1, 0, 0, 0, time.UTC),
			zone:    "UTC",
			cutoff:  3,
			want:    date(2026, time.August, 4),
		},
		{
			name:    "after the cutoff is today",
			instant: time.Date(2026, 8, 5, 4, 0, 0, 0, time.UTC),
			zone:    "UTC",
			cutoff:  3,
			want:    date(2026, time.August, 5),
		},
		{
			// Israel shifts its clocks; the calendar date must still be the one
			// the user sees on their phone.
			name:    "dst zone resolves to the local calendar day",
			instant: time.Date(2026, 3, 27, 23, 30, 0, 0, time.UTC), // 02:30 Mar 28 IL
			zone:    "Asia/Jerusalem",
			want:    date(2026, time.March, 28),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := localDate(tt.instant, mustLoad(t, tt.zone), tt.cutoff)
			if !got.Equal(tt.want) {
				t.Errorf("localDate() = %s, want %s", got.Format(DateLayout), tt.want.Format(DateLayout))
			}
		})
	}
}

// A zone that no longer resolves must not lock a user out of their own habit.
func TestLoadLocation_FallsBackToUTC(t *testing.T) {
	for _, tz := range []string{"", "Mars/Olympus_Mons", "not a zone"} {
		if got := loadLocation(tz); got != time.UTC {
			t.Errorf("loadLocation(%q) = %v, want UTC", tz, got)
		}
	}
}

func TestWeekStart(t *testing.T) {
	tests := []struct {
		name string
		in   time.Time
		want time.Time
	}{
		{name: "monday is its own start", in: date(2026, time.August, 3), want: date(2026, time.August, 3)},
		{name: "midweek", in: date(2026, time.August, 5), want: date(2026, time.August, 3)},
		// Sunday is the end of the week here, not the start — the boundary
		// decides which check-ins count toward a quota.
		{name: "sunday belongs to the week that opened", in: date(2026, time.August, 9), want: date(2026, time.August, 3)},
		{name: "crosses a month", in: date(2026, time.September, 1), want: date(2026, time.August, 31)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := weekStart(tt.in, nil); !got.Equal(tt.want) {
				t.Errorf("weekStart(%s) = %s, want %s",
					tt.in.Format(DateLayout), got.Format(DateLayout), tt.want.Format(DateLayout))
			}
		})
	}
}

// The boundary follows users.week_start because the work week is Sun–Thu in
// Israel and the product ships Hebrew. A Sunday-start user whose habit weeks
// began on Monday would see "3x this week" straddle two of their weeks.
func TestWeekStart_FollowsTheUserPreference(t *testing.T) {
	wednesday := date(2026, time.August, 5)

	tests := []struct {
		name     string
		firstDay *int
		want     time.Time
	}{
		{name: "monday start", firstDay: intPtr(1), want: date(2026, time.August, 3)},
		{name: "sunday start", firstDay: intPtr(0), want: date(2026, time.August, 2)},
		{name: "saturday start", firstDay: intPtr(6), want: date(2026, time.August, 1)},
		// Sunday is 0 and so is the zero value, so an unstamped habit must fall
		// back to Monday rather than silently becoming Sunday-start.
		{name: "unset falls back to monday", firstDay: nil, want: date(2026, time.August, 3)},
		{name: "out of range falls back to monday", firstDay: intPtr(9), want: date(2026, time.August, 3)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := weekStart(wednesday, tt.firstDay); !got.Equal(tt.want) {
				t.Errorf("weekStart = %s, want %s", got.Format(DateLayout), tt.want.Format(DateLayout))
			}
		})
	}
}

// A day that IS the boundary is its own week start, not the previous week's.
func TestWeekStart_OnTheBoundaryItself(t *testing.T) {
	sunday := date(2026, time.August, 2)

	if got := weekStart(sunday, intPtr(0)); !got.Equal(sunday) {
		t.Errorf("weekStart = %s, want the Sunday itself", got.Format(DateLayout))
	}
}

func TestIsScheduledOn(t *testing.T) {
	monWedFri := Habit{ScheduleKind: ScheduleWeekdays, ByWeekday: []int16{1, 3, 5}}

	tests := []struct {
		name string
		h    Habit
		d    time.Time
		want bool
	}{
		{name: "weekdays habit on a scheduled day", h: monWedFri, d: date(2026, time.August, 3), want: true},
		{name: "weekdays habit on an unscheduled day", h: monWedFri, d: date(2026, time.August, 4), want: false},
		{name: "daily habit is always due", h: Habit{ScheduleKind: ScheduleDaily}, d: date(2026, time.August, 4), want: true},
		{
			// A quota habit has no unscheduled days; the quota, not the
			// calendar, decides when it stops being due.
			name: "quota habit is due on any day",
			h:    Habit{ScheduleKind: ScheduleWeeklyQuota, TimesPerWeek: i16Ptr(3)},
			d:    date(2026, time.August, 4),
			want: true,
		},
		{
			name: "sunday maps to weekday zero",
			h:    Habit{ScheduleKind: ScheduleWeekdays, ByWeekday: []int16{0}},
			d:    date(2026, time.August, 9),
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsScheduledOn(tt.h, tt.d); got != tt.want {
				t.Errorf("IsScheduledOn() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSatisfies(t *testing.T) {
	tests := []struct {
		name     string
		polarity string
		value    int
		target   int
		want     bool
	}{
		{name: "build habit meets its target", polarity: PolarityBuild, value: 20, target: 20, want: true},
		{name: "build habit exceeds its target", polarity: PolarityBuild, value: 25, target: 20, want: true},
		{name: "build habit falls short", polarity: PolarityBuild, value: 19, target: 20, want: false},
		// The inversion that makes "quit drinking" work: zero is success and
		// logging a slip is the failure.
		{name: "quit habit stays clean", polarity: PolarityQuit, value: 0, target: 0, want: true},
		{name: "quit habit slips", polarity: PolarityQuit, value: 1, target: 0, want: false},
		{name: "quit habit within an allowance", polarity: PolarityQuit, value: 2, target: 2, want: true},
		{name: "quit habit over its allowance", polarity: PolarityQuit, value: 3, target: 2, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := satisfies(tt.polarity, tt.value, tt.target); got != tt.want {
				t.Errorf("satisfies() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateCheckInDate(t *testing.T) {
	today := date(2026, time.August, 5) // a Wednesday
	daily := Habit{ScheduleKind: ScheduleDaily}
	quota := Habit{ScheduleKind: ScheduleWeeklyQuota, TimesPerWeek: i16Ptr(3)}

	tests := []struct {
		name    string
		h       Habit
		d       time.Time
		wantErr bool
	}{
		{name: "today", h: daily, d: today},
		{name: "yesterday", h: daily, d: date(2026, time.August, 4)},
		{name: "the last day inside the window", h: daily, d: date(2026, time.July, 29)},
		{name: "one day past the window", h: daily, d: date(2026, time.July, 28), wantErr: true},
		{name: "tomorrow", h: daily, d: date(2026, time.August, 6), wantErr: true},
		{name: "far future", h: daily, d: date(2027, time.January, 1), wantErr: true},

		{name: "quota, current week", h: quota, d: date(2026, time.August, 3)},
		{name: "quota, previous week", h: quota, d: date(2026, time.July, 27)},
		// A quota week closes and locks: allowing edits two weeks back would let
		// a user rebuild a streak that never happened.
		{name: "quota, two weeks back", h: quota, d: date(2026, time.July, 26), wantErr: true},
		{name: "quota, future", h: quota, d: date(2026, time.August, 6), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCheckInDate(tt.h, tt.d, today)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateCheckInDate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestParseDate(t *testing.T) {
	got, err := parseDate("2026-08-05")
	if err != nil {
		t.Fatalf("parseDate: %v", err)
	}
	if !got.Equal(date(2026, time.August, 5)) {
		t.Errorf("parseDate = %s, want 2026-08-05 at UTC midnight", got)
	}

	for _, bad := range []string{"05/08/2026", "2026-8-5T00:00:00Z", "yesterday", ""} {
		if _, err := parseDate(bad); err == nil {
			t.Errorf("parseDate(%q) succeeded, want an error", bad)
		}
	}
}
