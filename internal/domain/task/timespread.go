package task

import "time"

// TimeSpreadResponse is the three-bucket view for the Time Spread screens.
type TimeSpreadResponse struct {
	Today    []TaskView `json:"today"`
	Tomorrow []TaskView `json:"tomorrow"`
	ThisWeek []TaskView `json:"thisWeek"`
}

// bucketTimeSpread sorts the candidate set (already active-only) into
// today / tomorrow / this-week relative to `now`, applying the no-guilt
// roll-forward. Pure: `now` is injected, never read here.
//
// Placement, first match wins:
//   - a recurring occurrence past its date → no bucket (see placeTask)
//   - a scheduledFor for today, OR a past scheduledFor that rollsOver → today
//   - scheduledFor tomorrow → tomorrow
//   - scheduledFor within the rest of this week (≤ 6 days out) → thisWeek
//   - a past scheduledFor with rollsOver=false → dropped (no bucket)
//   - anything else (unscheduled / far future) → no bucket
func bucketTimeSpread(candidates []Task, now time.Time) TimeSpreadResponse {
	loc := now.Location()
	today := dayStart(now)
	tomorrow := today.AddDate(0, 0, 1)
	weekEnd := today.AddDate(0, 0, 7) // exclusive upper bound for "this week"

	resp := TimeSpreadResponse{Today: []TaskView{}, Tomorrow: []TaskView{}, ThisWeek: []TaskView{}}

	for _, t := range candidates {
		switch placeTask(t, loc, today, tomorrow, weekEnd) {
		case bucketToday:
			resp.Today = append(resp.Today, TaskToView(t))
		case bucketTomorrow:
			resp.Tomorrow = append(resp.Tomorrow, TaskToView(t))
		case bucketThisWeek:
			resp.ThisWeek = append(resp.ThisWeek, TaskToView(t))
		case bucketNone:
			// not in any time bucket
		}
	}
	return resp
}

type bucket int

const (
	bucketNone bucket = iota
	bucketToday
	bucketTomorrow
	bucketThisWeek
)

func placeTask(t Task, loc *time.Location, today, tomorrow, weekEnd time.Time) bucket {
	if t.ScheduledFor == nil {
		return bucketNone // unscheduled → not in any time bucket
	}
	sched, err := time.ParseInLocation(scheduledForLayout, *t.ScheduledFor, loc)
	if err != nil {
		return bucketNone
	}
	schedDay := dayStart(sched)
	switch {
	case schedDay.Before(today):
		// A recurring occurrence is an appointment with a window, not a debt that
		// follows you: once its day passes it leaves Today regardless of
		// rollsOver, which is meaningless on this path. It stays completable from
		// the project view and search until the sweep reaps it to `missed`.
		if t.RecurrenceRuleID != nil {
			return bucketNone
		}
		if t.RollsOver {
			return bucketToday // carried over, no guilt
		}
		return bucketNone // missed and doesn't roll → drops off
	case schedDay.Equal(today):
		return bucketToday
	case schedDay.Equal(tomorrow):
		return bucketTomorrow
	case schedDay.Before(weekEnd):
		return bucketThisWeek
	default:
		return bucketNone
	}
}
