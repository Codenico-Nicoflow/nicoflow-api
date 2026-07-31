package recurrence

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// maxInterval matches the DB CHECK — an interval past a year of days is a typo,
// not an intent.
const maxInterval = 366

// The scheduled_time contract, duplicated from the task domain rather than
// imported — the dependency runs the other way (task consumes recurrence via the
// Materializer seam), exactly as the occurrence status constants are duplicated.
const (
	scheduledTimeLayout = "15:04"
	scheduleSnapMinutes = 15
	dayEndMinutes       = 23*60 + 59
)

// clampEstimateToDayEnd caps an estimate so a timed occurrence cannot cross
// midnight — the same invariant the task domain enforces, applied at the
// template so no materialized row can ever violate it.
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

// validateScheduledTime rejects a malformed or non-snapped time. Nil means
// all-day and passes on every plan.
func validateScheduledTime(scheduledTime *string) error {
	if scheduledTime == nil {
		return nil
	}
	t, err := time.Parse(scheduledTimeLayout, *scheduledTime)
	if err != nil {
		return errInvalidInput("scheduledTime must be a 24-hour time (HH:MM)")
	}
	if t.Minute()%scheduleSnapMinutes != 0 {
		return errInvalidInput("scheduledTime must fall on a 15-minute boundary")
	}
	return nil
}

// enforceTimedSchedulingPlan gates SETTING a time to Pro, mirroring the task
// domain. Clearing is open on every plan so a downgraded user is never trapped
// holding a rule they cannot edit.
func enforceTimedSchedulingPlan(plan string, scheduledTime *string) error {
	if scheduledTime == nil || plan != planFree {
		return nil
	}
	return errPlanLimit("timed scheduling is a Pro feature")
}

func errPlanLimit(msg string) error {
	return apperror.New(http.StatusForbidden, apperror.ErrPlanLimitExceeded, msg)
}

func errInvalidRecurrence(msg string) error {
	return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidRecurrence, msg)
}

func errInvalidInput(msg string) error {
	return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, msg)
}

func errRuleNotFound() error {
	return apperror.New(http.StatusNotFound, apperror.ErrRecurrenceRuleNotFound, "recurrence rule not found")
}

// validateSchedule checks the schedule half of a rule. Fields belonging to a
// different frequency are rejected rather than ignored, so a client can't believe
// it set a constraint the engine will never read.
func validateSchedule(freq string, interval int, byWeekday []int, byMonthday *int) error {
	if !allowedFreqs[freq] {
		return errInvalidRecurrence("freq must be one of: daily, weekly, monthly, yearly")
	}
	if interval < 1 || interval > maxInterval {
		return errInvalidRecurrence("interval must be between 1 and 366")
	}

	if len(byWeekday) > 0 {
		if freq != FreqWeekly {
			return errInvalidRecurrence("byWeekday is only valid for a weekly rule")
		}
		seen := make(map[int]bool, len(byWeekday))
		for _, d := range byWeekday {
			if d < 0 || d > 6 {
				return errInvalidRecurrence("byWeekday values must be between 0 (Sunday) and 6 (Saturday)")
			}
			if seen[d] {
				return errInvalidRecurrence("byWeekday must not contain duplicates")
			}
			seen[d] = true
		}
	}

	if byMonthday != nil {
		if freq != FreqMonthly {
			return errInvalidRecurrence("byMonthday is only valid for a monthly rule")
		}
		if *byMonthday != MonthdayLast && (*byMonthday < 1 || *byMonthday > 31) {
			return errInvalidRecurrence("byMonthday must be between 1 and 31, or -1 for the last day of the month")
		}
	}
	return nil
}

// validateCreate runs every check a create must pass and returns the parsed
// window. Grouped here so the service's Create reads as orchestration.
func validateCreate(req CreateRuleRequest) (start time.Time, end *time.Time, err error) {
	if err := validateTemplate(req.Title, req.Notes, req.Priority, req.Energy, req.EstimatedMinutes); err != nil {
		return time.Time{}, nil, err
	}
	if err := validateSchedule(req.Freq, req.Interval, req.ByWeekday, req.ByMonthday); err != nil {
		return time.Time{}, nil, err
	}
	if err := validateScheduledTime(req.ScheduledTime); err != nil {
		return time.Time{}, nil, err
	}
	start, err = parseDate("startDate", req.StartDate)
	if err != nil {
		return time.Time{}, nil, err
	}
	if req.EndDate != nil {
		d, err := parseDate("endDate", *req.EndDate)
		if err != nil {
			return time.Time{}, nil, err
		}
		end = &d
	}
	if err := validateDates(start, end); err != nil {
		return time.Time{}, nil, err
	}
	return start, end, nil
}

// validateUpdate runs every check an edited rule must pass.
func validateUpdate(r Rule) error {
	if err := validateTemplate(r.Title, r.Notes, r.Priority, r.Energy, r.EstimatedMinutes); err != nil {
		return err
	}
	if err := validateSchedule(r.Freq, r.Interval, r.ByWeekday, r.ByMonthday); err != nil {
		return err
	}
	if err := validateScheduledTime(r.ScheduledTime); err != nil {
		return err
	}
	return validateDates(r.StartDate, r.EndDate)
}

// validateDates checks the series window. An end before the start is rejected
// outright: it would produce a rule that can never fire.
func validateDates(start time.Time, end *time.Time) error {
	if end != nil && end.Before(start) {
		return errInvalidRecurrence("endDate must not be before startDate")
	}
	return nil
}

// validateTemplate checks the task-template half, mirroring the task domain's
// own limits so a materialized occurrence can never violate them.
func validateTemplate(title string, notes *string, priority, energy string, estimatedMinutes *int) error {
	if strings.TrimSpace(title) == "" {
		return errInvalidInput("title is required")
	}
	if len(title) > maxTitleLen {
		return errInvalidInput("title must be 255 characters or fewer")
	}
	if notes != nil && len(*notes) > maxNotesLen {
		return errInvalidInput("notes must be 2000 characters or fewer")
	}
	if !allowedPriorities[priority] {
		return errInvalidInput("priority must be one of: low, medium, high")
	}
	if !allowedEnergies[energy] {
		return errInvalidInput("energy must be one of: low, medium, deep")
	}
	if estimatedMinutes != nil && (*estimatedMinutes < 0 || *estimatedMinutes > 24*60) {
		return errInvalidInput("estimatedMinutes must be between 0 and 1440")
	}
	return nil
}

// normalizeWeekdays sorts and de-nils the weekday set so storage and the wire
// shape are stable regardless of the order the client sent.
func normalizeWeekdays(days []int) []int {
	if len(days) == 0 {
		return nil
	}
	out := append([]int(nil), days...)
	sort.Ints(out)
	return out
}

// parseDate parses a required YYYY-MM-DD field.
func parseDate(field, value string) (time.Time, error) {
	d, err := ParseDate(value)
	if err != nil {
		return time.Time{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidDate, field+" must be a valid date (YYYY-MM-DD)")
	}
	return d, nil
}
