package task

import "time"

// TimeSpreadResponse is the three-bucket view for the Time Spread screens.
type TimeSpreadResponse struct {
	Today    []TaskView `json:"today"`
	Tomorrow []TaskView `json:"tomorrow"`
	ThisWeek []TaskView `json:"thisWeek"`
}

// bucketTimeSpread sorts the candidate set (already active+inbox only) into
// today / tomorrow / this-week relative to `now`, applying the no-guilt
// roll-forward. Pure: `now` is injected, never read here.
//
// Placement, first match wins:
//   - a soft scheduledFor for today, OR a past scheduledFor that rollsOver,
//     OR a hard dueDate that is today or in the past → today
//   - scheduledFor or dueDate tomorrow → tomorrow
//   - scheduledFor or dueDate within the rest of this week (≤ 6 days out) → thisWeek
//   - a past soft scheduledFor with rollsOver=false → dropped (no bucket)
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
	// Soft scheduled day (parsed in the user's location) takes precedence for
	// roll-forward semantics.
	if t.ScheduledFor != nil {
		if sched, err := time.ParseInLocation(scheduledForLayout, *t.ScheduledFor, loc); err == nil {
			schedDay := dayStart(sched)
			switch {
			case schedDay.Before(today):
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
	}

	// Hard due date: a past or today due always surfaces today (tone is the FE's job).
	if t.DueDate != nil {
		dueDay := dayStart(t.DueDate.In(loc))
		switch {
		case !dueDay.After(today): // today or past
			return bucketToday
		case dueDay.Equal(tomorrow):
			return bucketTomorrow
		case dueDay.Before(weekEnd):
			return bucketThisWeek
		default:
			return bucketNone
		}
	}

	return bucketNone // unscheduled, no hard due → not in any time bucket
}
