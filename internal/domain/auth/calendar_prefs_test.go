package auth

import (
	"errors"
	"net/http"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

func intPtr(v int) *int { return &v }

// stored is a user's existing window, used to resolve a partial update.
var stored = CalendarPrefs{WeekStart: 1, Workdays: []int{1, 2, 3, 4, 5}, DayStartHour: 8, DayEndHour: 18}

func TestValidateCalendarPrefs(t *testing.T) {
	tests := []struct {
		name    string
		req     UpdateMeRequest
		wantErr bool
	}{
		{name: "no calendar fields is a no-op"},
		{name: "sunday week start", req: UpdateMeRequest{WeekStart: intPtr(0)}},
		{name: "saturday week start", req: UpdateMeRequest{WeekStart: intPtr(6)}},
		{name: "week start below range", req: UpdateMeRequest{WeekStart: intPtr(-1)}, wantErr: true},
		{name: "week start above range", req: UpdateMeRequest{WeekStart: intPtr(7)}, wantErr: true},

		{name: "european work week", req: UpdateMeRequest{Workdays: []int{1, 2, 3, 4, 5}}},
		// The app ships Hebrew + RTL, so this is a first-class case, not an edge one.
		{name: "israeli work week", req: UpdateMeRequest{Workdays: []int{0, 1, 2, 3, 4}}},
		{name: "single day", req: UpdateMeRequest{Workdays: []int{3}}},
		{name: "all seven days", req: UpdateMeRequest{Workdays: []int{0, 1, 2, 3, 4, 5, 6}}},
		// A calendar with no days is a blank screen the user cannot navigate out of.
		{name: "empty workdays", req: UpdateMeRequest{Workdays: []int{}}, wantErr: true},
		{name: "workday out of range", req: UpdateMeRequest{Workdays: []int{1, 7}}, wantErr: true},
		{name: "negative workday", req: UpdateMeRequest{Workdays: []int{-1}}, wantErr: true},
		// Duplicates mean the client built the set wrongly; collapsing them hides that.
		{name: "duplicate workday", req: UpdateMeRequest{Workdays: []int{1, 1, 2}}, wantErr: true},

		{name: "full day window", req: UpdateMeRequest{DayStartHour: intPtr(0), DayEndHour: intPtr(24)}},
		// 24 is exclusive — this is the "08:00 to midnight" the feature exists for.
		{name: "window through midnight", req: UpdateMeRequest{DayStartHour: intPtr(8), DayEndHour: intPtr(24)}},
		{name: "start below range", req: UpdateMeRequest{DayStartHour: intPtr(-1), DayEndHour: intPtr(12)}, wantErr: true},
		{name: "start at 24 leaves no hours", req: UpdateMeRequest{DayStartHour: intPtr(24)}, wantErr: true},
		{name: "end above range", req: UpdateMeRequest{DayStartHour: intPtr(0), DayEndHour: intPtr(25)}, wantErr: true},
		{name: "end at zero", req: UpdateMeRequest{DayEndHour: intPtr(0)}, wantErr: true},
		{name: "inverted window", req: UpdateMeRequest{DayStartHour: intPtr(18), DayEndHour: intPtr(8)}, wantErr: true},
		{name: "empty window", req: UpdateMeRequest{DayStartHour: intPtr(9), DayEndHour: intPtr(9)}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCalendarPrefs(tt.req, stored)
			if tt.wantErr {
				assertInvalidInput(t, err)
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// A half-specified window has to be judged against what is already stored, or a
// request that lands an empty range passes every per-field check and fails in
// the database as a 500.
func TestValidateCalendarPrefs_PartialWindow(t *testing.T) {
	tests := []struct {
		name    string
		req     UpdateMeRequest
		wantErr bool
	}{
		{name: "new start before stored end", req: UpdateMeRequest{DayStartHour: intPtr(6)}},
		{name: "new start after stored end", req: UpdateMeRequest{DayStartHour: intPtr(20)}, wantErr: true},
		{name: "new start equal to stored end", req: UpdateMeRequest{DayStartHour: intPtr(18)}, wantErr: true},
		{name: "new end after stored start", req: UpdateMeRequest{DayEndHour: intPtr(22)}},
		{name: "new end before stored start", req: UpdateMeRequest{DayEndHour: intPtr(6)}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCalendarPrefs(tt.req, stored)
			if tt.wantErr {
				assertInvalidInput(t, err)
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

// A workdays-only change must not be measured against a zero-valued window that
// was never read from the database.
func TestValidateCalendarPrefs_WorkdaysOnlyIgnoresWindow(t *testing.T) {
	if err := validateCalendarPrefs(UpdateMeRequest{Workdays: []int{1, 2}}, CalendarPrefs{}); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func assertInvalidInput(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	// The DB constraints are a backstop; the client must get a typed 422 rather
	// than a Postgres error string in a 500.
	var appErr *apperror.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error is not an AppError: %v", err)
	}
	if appErr.Status != http.StatusUnprocessableEntity {
		t.Errorf("status = %d, want 422", appErr.Status)
	}
	if appErr.Code != apperror.ErrInvalidInput {
		t.Errorf("code = %q, want %q", appErr.Code, apperror.ErrInvalidInput)
	}
}
