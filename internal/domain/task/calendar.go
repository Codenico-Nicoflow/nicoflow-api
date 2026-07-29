package task

import (
	"net/http"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/optional"
)

const (
	// scheduledTimeLayout is the wire format for scheduled_time: HH:MM, 24-hour.
	// Seconds are deliberately absent — the grid snaps to 15 minutes, so storing
	// or returning them would only invite drift.
	scheduledTimeLayout = "15:04"

	// scheduleSnapMinutes is the calendar's placement granularity. Enforced on
	// write so the stored value can never disagree with what the grid can draw.
	scheduleSnapMinutes = 15

	// dayEndMinutes is 23:59 — the clamp ceiling. A task belongs to its
	// scheduled_for day, so a start+estimate that would cross midnight is capped
	// rather than spilling into a day the row does not claim.
	dayEndMinutes = 23*60 + 59

	// maxRangeSpanDays caps a single calendar query. A month view needs ~31 days
	// and a padded month grid ~42; 62 leaves room for two months while keeping
	// the response bounded.
	maxRangeSpanDays = 62
)

// DateRange is the parsed, validated window for the calendar query.
type DateRange struct {
	From string // inclusive ISO date
	To   string // inclusive ISO date
}

// parseDateRange validates both bounds and the span. Both are required: an
// unbounded calendar query would scan a user's whole history.
func parseDateRange(from, to string) (DateRange, error) {
	if from == "" || to == "" {
		return DateRange{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput,
			"scheduledFrom and scheduledTo are both required")
	}
	fromDay, err := time.Parse(scheduledForLayout, from)
	if err != nil {
		return DateRange{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput,
			"scheduledFrom must be an ISO date (YYYY-MM-DD)")
	}
	toDay, err := time.Parse(scheduledForLayout, to)
	if err != nil {
		return DateRange{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput,
			"scheduledTo must be an ISO date (YYYY-MM-DD)")
	}
	if toDay.Before(fromDay) {
		return DateRange{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput,
			"scheduledTo must not be before scheduledFrom")
	}
	// Inclusive span: from==to is one day, not zero.
	if int(toDay.Sub(fromDay).Hours()/24)+1 > maxRangeSpanDays {
		return DateRange{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput,
			"range must not span more than 62 days")
	}
	return DateRange{From: from, To: to}, nil
}

// enforceTimedSchedulingPlan gates SETTING a time to Pro. Clearing (nil) is
// open on every plan: a user who downgrades must never be trapped holding data
// they cannot edit. The plan comes from the JWT claim — the gate lives here in
// the service, not the router, because a hidden frontend control is not
// enforcement.
func enforceTimedSchedulingPlan(plan string, scheduledTime *string) error {
	if scheduledTime == nil || plan != planFree {
		return nil
	}
	return apperror.New(http.StatusForbidden, apperror.ErrPlanLimitExceeded,
		"timed scheduling is a Pro feature")
}

// validateScheduledTime rejects a malformed or non-snapped time. Nil means
// "unset" and passes — clearing a time is valid on every plan.
func validateScheduledTime(scheduledTime *string) error {
	if scheduledTime == nil {
		return nil
	}
	t, err := time.Parse(scheduledTimeLayout, *scheduledTime)
	if err != nil {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput,
			"scheduledTime must be a 24-hour time (HH:MM)")
	}
	if t.Minute()%scheduleSnapMinutes != 0 {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput,
			"scheduledTime must fall on a 15-minute boundary")
	}
	return nil
}

// clampUpdateEstimate resolves a PATCH's tri-state time/estimate against the
// stored row and clamps the result. Returns the estimate field to persist,
// which stays untouched when the effective pair cannot cross midnight.
func clampUpdateEstimate(req UpdateTaskRequest, current Task) optional.Field[int] {
	effectiveTime := current.ScheduledTime
	if req.ScheduledTime.Set {
		effectiveTime = req.ScheduledTime.Value
	}
	effectiveEstimate := current.EstimatedMinutes
	if req.EstimatedMinutes.Set {
		effectiveEstimate = req.EstimatedMinutes.Value
	}

	clamped := clampEstimateToDayEnd(effectiveTime, effectiveEstimate)
	if clamped == effectiveEstimate {
		return req.EstimatedMinutes // nothing to clamp — leave the field as sent
	}
	// The clamp bit: write it even when this PATCH only moved the time, since
	// the stored estimate would otherwise now cross midnight.
	return optional.Field[int]{Set: true, Value: clamped}
}

// clampEstimateToDayEnd caps an estimate so start+estimate cannot cross
// midnight. Cross-midnight is banned outright rather than split across two
// rows: day-keyed grouping, the notification sweeps and recurrence dedupe all
// assume a task lives on exactly one scheduled_for.
//
// Pure and total: an unset time or estimate is returned untouched, since
// without a start there is nothing to clamp against.
func clampEstimateToDayEnd(scheduledTime *string, estimatedMinutes *int) *int {
	if scheduledTime == nil || estimatedMinutes == nil {
		return estimatedMinutes
	}
	start, err := time.Parse(scheduledTimeLayout, *scheduledTime)
	if err != nil {
		return estimatedMinutes // malformed input is the validator's error to raise
	}
	startMinutes := start.Hour()*60 + start.Minute()
	if startMinutes+*estimatedMinutes <= dayEndMinutes {
		return estimatedMinutes
	}
	clamped := dayEndMinutes - startMinutes
	return &clamped
}
