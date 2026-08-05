package habit

import (
	"testing"
	"time"
)

// Streak derivation is the highest-risk logic in the feature: it produces a
// number rather than an error, so a wrong walk shows the user a confident lie
// instead of failing. These tests pin every date explicitly.

// ci builds a satisfied check-in on the given day unless satisfied is false.
func ci(d time.Time, value int, satisfied bool) CheckIn {
	return CheckIn{Date: d, Value: value, TargetAt: 1, Satisfied: satisfied}
}

// run builds `n` consecutive satisfied days ending on `end`.
func run(end time.Time, n int) []CheckIn {
	out := make([]CheckIn, 0, n)
	for i := n - 1; i >= 0; i-- {
		out = append(out, ci(end.AddDate(0, 0, -i), 1, true))
	}
	return out
}

func dailyH() Habit {
	return Habit{ID: "h1", Polarity: PolarityBuild, TargetValue: 1, ScheduleKind: ScheduleDaily}
}

func monWedFriH() Habit {
	return Habit{ID: "h1", Polarity: PolarityBuild, TargetValue: 1,
		ScheduleKind: ScheduleWeekdays, ByWeekday: []int16{1, 3, 5}}
}

func quotaH(times int16) Habit {
	return Habit{ID: "h1", Polarity: PolarityBuild, TargetValue: 1,
		ScheduleKind: ScheduleWeeklyQuota, TimesPerWeek: &times}
}

func TestDerive_DailyStreaks(t *testing.T) {
	today := date(2026, time.August, 5) // Wednesday

	tests := []struct {
		name        string
		checkIns    []CheckIn
		wantCurrent int
		wantLongest int
	}{
		{
			name:     "no history",
			checkIns: nil,
		},
		{
			name:        "checked in today only",
			checkIns:    []CheckIn{ci(today, 1, true)},
			wantCurrent: 1, wantLongest: 1,
		},
		{
			name:        "unbroken run ending today",
			checkIns:    run(today, 5),
			wantCurrent: 5, wantLongest: 5,
		},
		{
			// The rule that stops every user seeing zero each morning: today is
			// pending until it closes, so it does not end yesterday's run.
			name:        "run ending yesterday with today still open",
			checkIns:    run(today.AddDate(0, 0, -1), 10),
			wantCurrent: 10, wantLongest: 10,
		},
		{
			name:        "a gap ends the current run",
			checkIns:    append(run(today.AddDate(0, 0, -5), 4), ci(today, 1, true)),
			wantCurrent: 1, wantLongest: 4,
		},
		{
			// An unsatisfied row is a miss, not a blank. This is how a quit
			// habit's slip breaks a streak.
			name:        "an unsatisfied day breaks the run",
			checkIns:    append(run(today.AddDate(0, 0, -2), 3), ci(today.AddDate(0, 0, -1), 0, false), ci(today, 1, true)),
			wantCurrent: 1, wantLongest: 3,
		},
		{
			name:        "longest survives a broken current run",
			checkIns:    append(run(today.AddDate(0, 0, -10), 8), run(today, 2)...),
			wantCurrent: 2, wantLongest: 8,
		},
		{
			// Backfilling the gap is what makes the 7-day window worth having.
			name:        "backfilled gap joins two runs",
			checkIns:    run(today, 3),
			wantCurrent: 3, wantLongest: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := derive(dailyH(), tt.checkIns, today)
			if got.current != tt.wantCurrent {
				t.Errorf("current = %d, want %d", got.current, tt.wantCurrent)
			}
			if got.longest != tt.wantLongest {
				t.Errorf("longest = %d, want %d", got.longest, tt.wantLongest)
			}
		})
	}
}

// The single most important fairness rule: a Mon/Wed/Fri habit must not break
// on Tuesday. Without it every scheduled habit resets constantly.
func TestDerive_UnscheduledDaysDoNotBreakAStreak(t *testing.T) {
	wednesday := date(2026, time.August, 5)
	monday := date(2026, time.August, 3)

	got := derive(monWedFriH(), []CheckIn{ci(monday, 1, true), ci(wednesday, 1, true)}, wednesday)

	if got.current != 2 {
		t.Errorf("current = %d, want 2 — Tuesday is not scheduled and cannot be a miss", got.current)
	}
	if !got.dueToday {
		t.Error("dueToday = false on a Wednesday, want true for a Mon/Wed/Fri habit")
	}
}

func TestDerive_MissedScheduledDayBreaksTheStreak(t *testing.T) {
	friday := date(2026, time.August, 7)
	monday := date(2026, time.August, 3)

	// Monday done, Wednesday missed, Friday done.
	got := derive(monWedFriH(), []CheckIn{ci(monday, 1, true), ci(friday, 1, true)}, friday)

	if got.current != 1 {
		t.Errorf("current = %d, want 1 — the missed Wednesday ends the run", got.current)
	}
}

func TestDerive_NotDueOnAnUnscheduledDay(t *testing.T) {
	tuesday := date(2026, time.August, 4)

	got := derive(monWedFriH(), nil, tuesday)
	if got.dueToday {
		t.Error("dueToday = true on a Tuesday, want false for a Mon/Wed/Fri habit")
	}
}

func TestDerive_QuotaStreaks(t *testing.T) {
	today := date(2026, time.August, 5) // Wednesday of the week starting Aug 3
	thisWeek := date(2026, time.August, 3)
	lastWeek := date(2026, time.July, 27)
	twoWeeksAgo := date(2026, time.July, 20)

	// threeIn fills a week with n satisfied check-ins on consecutive days.
	threeIn := func(weekStart time.Time, n int) []CheckIn {
		out := make([]CheckIn, 0, n)
		for i := 0; i < n; i++ {
			out = append(out, ci(weekStart.AddDate(0, 0, i), 1, true))
		}
		return out
	}

	tests := []struct {
		name         string
		checkIns     []CheckIn
		wantCurrent  int
		wantLongest  int
		wantDone     bool
		wantProgress PeriodProgress
	}{
		{
			name:         "empty",
			wantProgress: PeriodProgress{Current: 0, Target: 3},
		},
		{
			name:         "quota met this week only",
			checkIns:     threeIn(thisWeek, 3),
			wantCurrent:  1,
			wantLongest:  1,
			wantDone:     true,
			wantProgress: PeriodProgress{Current: 3, Target: 3},
		},
		{
			// The open-period rule again, one unit up: an unmet current week
			// does not break the run behind it, because days remain.
			name:         "partial current week keeps the previous run",
			checkIns:     append(threeIn(lastWeek, 3), threeIn(thisWeek, 2)...),
			wantCurrent:  1,
			wantLongest:  1,
			wantProgress: PeriodProgress{Current: 2, Target: 3},
		},
		{
			name:         "two consecutive met weeks",
			checkIns:     append(threeIn(lastWeek, 3), threeIn(thisWeek, 3)...),
			wantCurrent:  2,
			wantLongest:  2,
			wantDone:     true,
			wantProgress: PeriodProgress{Current: 3, Target: 3},
		},
		{
			// A closed week that fell short ends the run — the whole point of
			// scoring a quota habit by week rather than by day.
			name:         "a missed week ends the run",
			checkIns:     append(append(threeIn(twoWeeksAgo, 3), threeIn(lastWeek, 2)...), threeIn(thisWeek, 3)...),
			wantCurrent:  1,
			wantLongest:  1,
			wantDone:     true,
			wantProgress: PeriodProgress{Current: 3, Target: 3},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := derive(quotaH(3), tt.checkIns, today)

			if got.current != tt.wantCurrent {
				t.Errorf("current = %d, want %d", got.current, tt.wantCurrent)
			}
			if got.longest != tt.wantLongest {
				t.Errorf("longest = %d, want %d", got.longest, tt.wantLongest)
			}
			if got.doneToday != tt.wantDone {
				t.Errorf("doneToday = %v, want %v", got.doneToday, tt.wantDone)
			}
			if got.progress == nil {
				t.Fatal("progress is nil, want it populated for a quota habit")
			}
			if *got.progress != tt.wantProgress {
				t.Errorf("progress = %+v, want %+v", *got.progress, tt.wantProgress)
			}
		})
	}
}

// A quota habit drops out of "due" once its week is met — it stops nagging for
// the rest of the week rather than asking for a fourth session.
func TestDerive_QuotaDueUntilTheWeekIsMet(t *testing.T) {
	today := date(2026, time.August, 5)
	weekStart := date(2026, time.August, 3)

	partial := derive(quotaH(3), []CheckIn{ci(weekStart, 1, true), ci(weekStart.AddDate(0, 0, 1), 1, true)}, today)
	if !partial.dueToday {
		t.Error("dueToday = false at 2 of 3, want true")
	}

	met := derive(quotaH(3), []CheckIn{
		ci(weekStart, 1, true), ci(weekStart.AddDate(0, 0, 1), 1, true), ci(weekStart.AddDate(0, 0, 2), 1, true),
	}, today)
	if met.dueToday {
		t.Error("dueToday = true at 3 of 3, want false once the quota is met")
	}
}

// Day habits have no accumulating period; a null progress must not be read as
// 0/0 by the client.
func TestDerive_DayHabitsHaveNoPeriodProgress(t *testing.T) {
	today := date(2026, time.August, 5)

	if got := derive(dailyH(), run(today, 3), today); got.progress != nil {
		t.Errorf("progress = %+v, want nil for a day habit", got.progress)
	}
}

func TestDerive_TodayValueAndCompletion(t *testing.T) {
	today := date(2026, time.August, 5)
	h := dailyH()
	h.TargetValue = 20

	partial := derive(h, []CheckIn{ci(today, 5, false)}, today)
	if partial.todayVal != 5 {
		t.Errorf("todayValue = %d, want 5", partial.todayVal)
	}
	if partial.doneToday {
		t.Error("completedToday = true on an unsatisfied day, want false")
	}

	full := derive(h, []CheckIn{ci(today, 20, true)}, today)
	if !full.doneToday {
		t.Error("completedToday = false on a satisfied day, want true")
	}
}

func TestStreakUnitFor(t *testing.T) {
	tests := []struct {
		kind string
		want string
	}{
		{kind: ScheduleDaily, want: StreakUnitDay},
		{kind: ScheduleWeekdays, want: StreakUnitDay},
		{kind: ScheduleWeeklyQuota, want: StreakUnitWeek},
	}

	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			if got := streakUnitFor(Habit{ScheduleKind: tt.kind}); got != tt.want {
				t.Errorf("streakUnitFor(%q) = %q, want %q", tt.kind, got, tt.want)
			}
		})
	}
}

// ── Heatmap cells ────────────────────────────────────────────────────────────

func TestBuildCells_DayHabit(t *testing.T) {
	today := date(2026, time.August, 5)
	cells := buildCells(dailyH(), run(today, 3), today, 7)

	if len(cells) != 7 {
		t.Fatalf("got %d cells, want 7", len(cells))
	}
	if cells[len(cells)-1].Date != today.Format(DateLayout) {
		t.Errorf("last cell = %s, want today %s — cells run oldest first",
			cells[len(cells)-1].Date, today.Format(DateLayout))
	}
	for _, c := range cells[4:] {
		if !c.Satisfied {
			t.Errorf("cell %s not satisfied, want the trailing 3-day run marked", c.Date)
		}
	}
	if cells[0].Satisfied {
		t.Errorf("cell %s satisfied, want the days before the run empty", cells[0].Date)
	}
}

// Unscheduled days are present and flagged, never omitted: the client draws them
// as a baseline, and a missing day would read as a gap the user caused.
func TestBuildCells_MarksUnscheduledDays(t *testing.T) {
	wednesday := date(2026, time.August, 5)
	cells := buildCells(monWedFriH(), nil, wednesday, 7)

	scheduled := map[string]bool{}
	for _, c := range cells {
		scheduled[c.Date] = c.Scheduled
	}

	if !scheduled["2026-08-05"] || !scheduled["2026-08-03"] {
		t.Error("Monday/Wednesday not marked scheduled, want true")
	}
	if scheduled["2026-08-04"] {
		t.Error("Tuesday marked scheduled, want false for a Mon/Wed/Fri habit")
	}
}

func TestBuildCells_QuotaHabitEmitsWeekCells(t *testing.T) {
	today := date(2026, time.August, 5)
	thisWeek := date(2026, time.August, 3)

	cells := buildCells(quotaH(3), []CheckIn{
		ci(thisWeek, 1, true), ci(thisWeek.AddDate(0, 0, 1), 1, true),
	}, today, 28)

	if len(cells) != 4 {
		t.Fatalf("got %d cells, want 4 week cells for a 28-day window", len(cells))
	}

	last := cells[len(cells)-1]
	if last.Date != thisWeek.Format(DateLayout) {
		t.Errorf("last cell = %s, want the current week's Monday %s", last.Date, thisWeek.Format(DateLayout))
	}
	if last.Progress == nil || last.Progress.Current != 2 || last.Progress.Target != 3 {
		t.Errorf("progress = %+v, want 2 of 3", last.Progress)
	}
	if last.Satisfied {
		t.Error("week marked satisfied at 2 of 3, want false")
	}
	if !last.Scheduled {
		t.Error("week cell not scheduled, want true — a quota week is always on")
	}
}
