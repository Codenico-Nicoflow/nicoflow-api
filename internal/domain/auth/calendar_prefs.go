package auth

import (
	"context"
	"fmt"
	"net/http"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// Calendar display preference validation (NIC-1890).
//
// The DB carries CHECK constraints for all of these, but a constraint violation
// surfaces as a 500 with a Postgres error string. Validating here turns each one
// into a typed 422 the client can act on, and keeps the constraints as the
// backstop they are meant to be rather than the primary gate.

const (
	// daysInWeek bounds every weekday value: 0=Sunday … 6=Saturday, matching JS
	// getDay() and Go time.Weekday.
	daysInWeek = 7
	// maxDayEndHour is exclusive, so 24 means "through midnight". An inclusive
	// 23 could not express a window ending at 00:00.
	maxDayEndHour = 24
)

// validateCalendarUpdate validates the request's calendar fields, reading the
// stored preferences only when it actually needs them.
//
// The read is skipped when no calendar field is present, so the common profile
// update (a name, a theme) still costs exactly one query.
func (s *service) validateCalendarUpdate(ctx context.Context, userID string, req UpdateMeRequest) error {
	if req.WeekStart == nil && req.Workdays == nil && req.DayStartHour == nil && req.DayEndHour == nil {
		return nil
	}

	// Only a partial hour window needs the stored value; anything else can be
	// judged on its own.
	needsCurrent := (req.DayStartHour == nil) != (req.DayEndHour == nil)
	var current CalendarPrefs
	if needsCurrent {
		user, err := s.repo.GetUserByID(ctx, userID)
		if err != nil {
			return err
		}
		current = user.Calendar
	}

	return validateCalendarPrefs(req, current)
}

// validateCalendarPrefs checks whichever calendar fields the request carries.
//
// Each field is independently optional, so a client changing only the day window
// never has to echo back a week start it did not read. The cross-field window
// check therefore has to resolve absent values against what is already stored —
// see validateDayWindow.
func validateCalendarPrefs(req UpdateMeRequest, current CalendarPrefs) error {
	if req.WeekStart != nil {
		if *req.WeekStart < 0 || *req.WeekStart >= daysInWeek {
			return invalidPref("weekStart must be 0 (Sunday) through 6 (Saturday)")
		}
	}

	if req.Workdays != nil {
		if err := validateWorkdays(req.Workdays); err != nil {
			return err
		}
	}

	return validateDayWindow(req, current)
}

// validateWorkdays rejects an empty, out-of-range or duplicated set.
//
// Empty is rejected rather than treated as "hide everything": a calendar with no
// days is a blank screen, and a user who reaches it has no way back except to
// find this setting again.
func validateWorkdays(days []int) error {
	if len(days) == 0 {
		return invalidPref("workdays must contain at least one day")
	}

	seen := make(map[int]bool, len(days))
	for _, day := range days {
		if day < 0 || day >= daysInWeek {
			return invalidPref(fmt.Sprintf("workdays must contain values 0 through 6, got %d", day))
		}
		// Duplicates are rejected rather than silently collapsed: they mean the
		// client built the set wrongly, and accepting them hides that.
		if seen[day] {
			return invalidPref(fmt.Sprintf("workdays contains %d more than once", day))
		}
		seen[day] = true
	}
	return nil
}

// validateDayWindow checks the drawn hour range, including the case where only
// one end of it is being changed.
//
// Resolving the absent end against the STORED value is the point: a request
// setting only dayStartHour=20 against a stored end of 18 would otherwise pass
// every per-field check and land an empty window that the CHECK constraint
// rejects as a 500.
func validateDayWindow(req UpdateMeRequest, current CalendarPrefs) error {
	if req.DayStartHour == nil && req.DayEndHour == nil {
		// Nothing to judge. Returning early also keeps a workdays-only request
		// from being measured against a zero-valued `current` that was never read.
		return nil
	}

	start, end := current.DayStartHour, current.DayEndHour

	if req.DayStartHour != nil {
		if *req.DayStartHour < 0 || *req.DayStartHour >= maxDayEndHour {
			return invalidPref("dayStartHour must be 0 through 23")
		}
		start = *req.DayStartHour
	}
	if req.DayEndHour != nil {
		if *req.DayEndHour < 1 || *req.DayEndHour > maxDayEndHour {
			return invalidPref("dayEndHour must be 1 through 24")
		}
		end = *req.DayEndHour
	}

	if start >= end {
		return invalidPref("dayStartHour must be before dayEndHour")
	}
	return nil
}

func invalidPref(message string) error {
	return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, message)
}
