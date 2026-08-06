package habit

import "time"

// Streak derivation.
//
// A streak counts consecutive **satisfied periods**, and the habit's
// schedule_kind decides what a period is: a day for daily/weekdays, a week for
// weekly_quota. That one abstraction is what lets three schedule kinds share a
// single walk instead of three parallel implementations.
//
// Nothing here is stored. Streaks are derived on every read because the backfill
// window (NIC-1924) makes retroactive writes normal, and a stored counter cannot
// absorb those — it counts writes, not completions, and drifts the moment either
// path retries. Migration 037 records the same reasoning for recurrence rules.
//
// Cost is negligible at realistic volume: 3 habits over 2 years is ~2,200 rows,
// and each habit's history is walked once.

// HistoryWindow is how far back a read loads check-ins. Wide enough for the
// 30-cell ribbon plus the streak walk behind it, bounded so a multi-year habit
// never drags its whole history into a list response.
const HistoryWindow = 400

// RibbonDays is the heatmap window on a scalar read: 30 day cells, or the ~4
// months of week cells a quota habit renders in the same space.
const RibbonDays = 84

// ListRibbonDays is the heatmap window carried by a LIST read — narrower than
// the scalar's, because a board renders one ribbon per habit and only needs
// enough history to show a shape.
//
// Serving it costs nothing extra: the list already loads HistoryWindow days of
// check-ins per habit to derive the streaks, and without this the rows are
// walked once and discarded. This is the same data, kept rather than thrown
// away — not a second query.
//
// 14 days on a phone, which is what ribbonWindowSize renders at its narrowest;
// wider viewports show the same 14 with more room per cell rather than asking
// the server for more.
const ListRibbonDays = 14

// streakUnitFor reports which noun a habit's streak counts in.
func streakUnitFor(h Habit) string {
	if h.ScheduleKind == ScheduleWeeklyQuota {
		return StreakUnitWeek
	}
	return StreakUnitDay
}

// stats is everything a read derives from one habit's check-in history.
type stats struct {
	current   int
	longest   int
	dueToday  bool
	doneToday bool
	// loggedToday is whether an entry EXISTS for today, which is a different
	// question from doneToday (is the period satisfied). For a quota habit at
	// 1 of 3 the week is unmet but today is logged, and a ring that conflates
	// the two re-checks-in instead of undoing.
	loggedToday bool
	todayVal    int
	progress    *PeriodProgress
}

// derive computes a habit's counters from its check-ins as of today.
//
// checkIns may arrive in any order; only their dates matter.
func derive(h Habit, checkIns []CheckIn, today time.Time) stats {
	if h.ScheduleKind == ScheduleWeeklyQuota {
		return deriveQuota(h, checkIns, today)
	}
	return deriveDaily(h, checkIns, today)
}

// byDate indexes check-ins for O(1) lookup while walking backwards.
func byDate(checkIns []CheckIn) map[string]CheckIn {
	m := make(map[string]CheckIn, len(checkIns))
	for _, c := range checkIns {
		m[c.Date.Format(DateLayout)] = c
	}
	return m
}

// deriveDaily walks day by day for daily and weekdays habits.
//
// Two rules make the number feel fair rather than punitive:
//
//   - Only *scheduled* days count. A Mon/Wed/Fri habit does not break on
//     Tuesday, which removes the most common cause of an unjust reset.
//   - Today never breaks a streak while it is still open. An unchecked today is
//     pending, not failed — otherwise every user would watch their streak read
//     zero each morning.
func deriveDaily(h Habit, checkIns []CheckIn, today time.Time) stats {
	idx := byDate(checkIns)
	s := stats{dueToday: IsScheduledOn(h, today)}

	if c, ok := idx[today.Format(DateLayout)]; ok {
		s.doneToday, s.todayVal, s.loggedToday = c.Satisfied, c.Value, true
	}

	// The current run: walk back from today, skipping unscheduled days, and stop
	// at the first scheduled day that was missed or failed.
	for d := today; !d.Before(earliest(checkIns, today)); d = d.AddDate(0, 0, -1) {
		if !IsScheduledOn(h, d) {
			continue
		}
		c, ok := idx[d.Format(DateLayout)]
		if ok && c.Satisfied {
			s.current++
			continue
		}
		// An open today is pending, so it neither extends nor ends the run.
		if d.Equal(today) {
			continue
		}
		break
	}

	s.longest = longestDailyRun(h, idx, checkIns, today, s.current)
	return s
}

// longestDailyRun scans the whole loaded history for the best run. The current
// streak is passed in because it can be the longest, and it is the one run the
// backwards walk already knows about.
func longestDailyRun(h Habit, idx map[string]CheckIn, checkIns []CheckIn, today time.Time, current int) int {
	best, run := current, 0
	for d := earliest(checkIns, today); !d.After(today); d = d.AddDate(0, 0, 1) {
		if !IsScheduledOn(h, d) {
			continue
		}
		c, ok := idx[d.Format(DateLayout)]
		switch {
		case ok && c.Satisfied:
			run++
			if run > best {
				best = run
			}
		case d.Equal(today):
			// Today is still open; it cannot end a historical run.
		default:
			run = 0
		}
	}
	return best
}

// deriveQuota walks week by week. A quota week succeeds when its satisfied
// check-ins reach timesPerWeek — no individual day can fail it, which is the
// point of "3 times a week, whichever days suit you".
func deriveQuota(h Habit, checkIns []CheckIn, today time.Time) stats {
	target := 1
	if h.TimesPerWeek != nil {
		target = int(*h.TimesPerWeek)
	}

	perWeek := map[string]int{}
	for _, c := range checkIns {
		if c.Satisfied {
			perWeek[weekStart(c.Date, h.WeekStart).Format(DateLayout)]++
		}
	}

	thisWeek := weekStart(today, h.WeekStart)
	done := perWeek[thisWeek.Format(DateLayout)]

	s := stats{
		// A quota habit stops being due once the week's quota is met; until
		// then it is due every day, whichever days the user picks.
		dueToday:  done < target,
		doneToday: done >= target,
		progress:  &PeriodProgress{Current: done, Target: target},
	}

	if c, ok := byDate(checkIns)[today.Format(DateLayout)]; ok {
		s.todayVal, s.loggedToday = c.Value, true
	}

	// The current run: this week counts only once its quota is met, but an
	// unmet *open* week does not break the run behind it — the user still has
	// days left to hit it.
	w := thisWeek
	if done < target {
		w = w.AddDate(0, 0, -7)
	}
	for ; !w.Before(weekStart(earliest(checkIns, today), h.WeekStart)); w = w.AddDate(0, 0, -7) {
		if perWeek[w.Format(DateLayout)] < target {
			break
		}
		s.current++
	}

	s.longest = longestQuotaRun(perWeek, checkIns, today, target, s.current, h.WeekStart)
	return s
}

func longestQuotaRun(perWeek map[string]int, checkIns []CheckIn, today time.Time, target, current int, firstDay *int) int {
	best, run := current, 0
	thisWeek := weekStart(today, firstDay)
	for w := weekStart(earliest(checkIns, today), firstDay); !w.After(thisWeek); w = w.AddDate(0, 0, 7) {
		met := perWeek[w.Format(DateLayout)] >= target
		switch {
		case met:
			run++
			if run > best {
				best = run
			}
		case w.Equal(thisWeek):
			// The current week is still open; it cannot end a historical run.
		default:
			run = 0
		}
	}
	return best
}

// earliest is the oldest date worth walking: the first check-in, or today when
// there is no history. Bounding the walk by real data keeps a habit created
// yesterday from scanning a year of empty days.
func earliest(checkIns []CheckIn, today time.Time) time.Time {
	oldest := today
	for _, c := range checkIns {
		if c.Date.Before(oldest) {
			oldest = c.Date
		}
	}
	return oldest
}

// buildCells renders the heatmap window at the habit's own granularity.
func buildCells(h Habit, checkIns []CheckIn, today time.Time, days int) []CellView {
	if h.ScheduleKind == ScheduleWeeklyQuota {
		return buildWeekCells(h, checkIns, today, days/7)
	}
	return buildDayCells(h, checkIns, today, days)
}

// buildDayCells emits one cell per day, oldest first.
//
// Unscheduled days are present with scheduled=false rather than omitted: the
// client needs them to draw a continuous ribbon, and a missing day would read as
// a gap the user is responsible for.
func buildDayCells(h Habit, checkIns []CheckIn, today time.Time, days int) []CellView {
	idx := byDate(checkIns)
	out := make([]CellView, 0, days)

	for i := days - 1; i >= 0; i-- {
		d := today.AddDate(0, 0, -i)
		cell := CellView{Date: d.Format(DateLayout), Scheduled: IsScheduledOn(h, d)}
		if c, ok := idx[cell.Date]; ok {
			cell.Value, cell.Satisfied = c.Value, c.Satisfied
		}
		out = append(out, cell)
	}
	return out
}

// buildWeekCells emits one cell per week, keyed by the week's Monday, carrying
// the quota progress the client renders as "2/3".
func buildWeekCells(h Habit, checkIns []CheckIn, today time.Time, weeks int) []CellView {
	target := 1
	if h.TimesPerWeek != nil {
		target = int(*h.TimesPerWeek)
	}

	perWeek := map[string]int{}
	for _, c := range checkIns {
		if c.Satisfied {
			perWeek[weekStart(c.Date, h.WeekStart).Format(DateLayout)]++
		}
	}

	thisWeek := weekStart(today, h.WeekStart)
	out := make([]CellView, 0, weeks)
	for i := weeks - 1; i >= 0; i-- {
		w := thisWeek.AddDate(0, 0, -7*i)
		key := w.Format(DateLayout)
		done := perWeek[key]
		out = append(out, CellView{
			Date:      key,
			Scheduled: true, // a quota week is always "on"
			Value:     done,
			Satisfied: done >= target,
			Progress:  &PeriodProgress{Current: done, Target: target},
		})
	}
	return out
}
