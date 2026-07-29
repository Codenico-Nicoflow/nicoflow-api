package task

import (
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/optional"
)

func TestParseDateRange(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		to       string
		wantErr  bool
		wantCode string
	}{
		{name: "single day is a valid inclusive range", from: "2026-08-01", to: "2026-08-01"},
		{name: "full month", from: "2026-08-01", to: "2026-08-31"},
		{name: "padded month grid of 42 days", from: "2026-07-27", to: "2026-09-06"},
		{name: "exactly 62 days is allowed", from: "2026-01-01", to: "2026-03-03"},
		{
			name: "63 days exceeds the cap", from: "2026-01-01", to: "2026-03-04",
			wantErr: true, wantCode: apperror.ErrInvalidInput,
		},
		{
			name: "missing from", from: "", to: "2026-08-31",
			wantErr: true, wantCode: apperror.ErrInvalidInput,
		},
		{
			name: "missing to", from: "2026-08-01", to: "",
			wantErr: true, wantCode: apperror.ErrInvalidInput,
		},
		{
			name: "non-ISO from", from: "August 1st", to: "2026-08-31",
			wantErr: true, wantCode: apperror.ErrInvalidInput,
		},
		{
			name: "reversed bounds", from: "2026-08-31", to: "2026-08-01",
			wantErr: true, wantCode: apperror.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDateRange(tt.from, tt.to)
			if tt.wantErr {
				ae := appErr(err)
				if ae == nil || ae.Code != tt.wantCode {
					t.Fatalf("want %s, got %+v", tt.wantCode, err)
				}
				if ae.Status != 422 {
					t.Errorf("status = %d, want 422", ae.Status)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.From != tt.from || got.To != tt.to {
				t.Errorf("range = %+v, want %s..%s", got, tt.from, tt.to)
			}
		})
	}
}

func TestValidateScheduledTime(t *testing.T) {
	tests := []struct {
		name    string
		value   *string
		wantErr bool
	}{
		{name: "nil passes — clearing is always valid", value: nil},
		{name: "midnight", value: ptr("00:00")},
		{name: "on the hour", value: ptr("09:00")},
		{name: "quarter past", value: ptr("09:15")},
		{name: "half past", value: ptr("09:30")},
		{name: "quarter to", value: ptr("09:45")},
		{name: "last snap of the day", value: ptr("23:45")},
		{name: "not on a 15-minute boundary", value: ptr("09:07"), wantErr: true},
		{name: "one minute off a boundary", value: ptr("09:16"), wantErr: true},
		{name: "hour out of range", value: ptr("24:00"), wantErr: true},
		{name: "minute out of range", value: ptr("09:60"), wantErr: true},
		{name: "seconds not accepted", value: ptr("09:00:00"), wantErr: true},
		{name: "12-hour clock not accepted", value: ptr("9:00 AM"), wantErr: true},
		{name: "empty string", value: ptr(""), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateScheduledTime(tt.value)
			if tt.wantErr {
				ae := appErr(err)
				if ae == nil || ae.Code != apperror.ErrInvalidInput {
					t.Fatalf("want INVALID_INPUT, got %+v", err)
				}
				if ae.Status != 422 {
					t.Errorf("status = %d, want 422", ae.Status)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestEnforceTimedSchedulingPlan(t *testing.T) {
	tests := []struct {
		name    string
		plan    string
		value   *string
		wantErr bool
	}{
		{name: "pro sets a time", plan: "pro", value: ptr("09:00")},
		{name: "free clears a time", plan: "free", value: nil},
		{name: "pro clears a time", plan: "pro", value: nil},
		{name: "free sets a time is gated", plan: "free", value: ptr("09:00"), wantErr: true},
		{name: "free setting midnight is still gated", plan: "free", value: ptr("00:00"), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := enforceTimedSchedulingPlan(tt.plan, tt.value)
			if tt.wantErr {
				ae := appErr(err)
				if ae == nil || ae.Code != apperror.ErrPlanLimitExceeded {
					t.Fatalf("want PLAN_LIMIT_EXCEEDED, got %+v", err)
				}
				if ae.Status != 403 {
					t.Errorf("status = %d, want 403", ae.Status)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestClampEstimateToDayEnd(t *testing.T) {
	tests := []struct {
		name      string
		time      *string
		estimate  *int
		wantValue *int
	}{
		{name: "fits inside the day", time: ptr("09:00"), estimate: ptr(60), wantValue: ptr(60)},
		{name: "ends exactly at 23:59", time: ptr("23:00"), estimate: ptr(59), wantValue: ptr(59)},
		{name: "would cross midnight is clamped", time: ptr("23:30"), estimate: ptr(60), wantValue: ptr(29)},
		{name: "starts at the last snap", time: ptr("23:45"), estimate: ptr(60), wantValue: ptr(14)},
		{name: "no time means nothing to clamp against", time: nil, estimate: ptr(600), wantValue: ptr(600)},
		{name: "no estimate stays nil", time: ptr("23:45"), estimate: nil, wantValue: nil},
		{name: "full day from midnight is clamped", time: ptr("00:00"), estimate: ptr(1440), wantValue: ptr(1439)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampEstimateToDayEnd(tt.time, tt.estimate)
			switch {
			case tt.wantValue == nil && got != nil:
				t.Fatalf("estimate = %d, want nil", *got)
			case tt.wantValue == nil:
				return
			case got == nil:
				t.Fatalf("estimate = nil, want %d", *tt.wantValue)
			case *got != *tt.wantValue:
				t.Errorf("estimate = %d, want %d", *got, *tt.wantValue)
			}
		})
	}
}

// clampUpdateEstimate has to resolve BOTH tri-state fields against the stored
// row: a PATCH that moves only the time must still clamp the estimate already
// on the task, or the row would silently start crossing midnight.
func TestClampUpdateEstimate(t *testing.T) {
	tests := []struct {
		name     string
		req      UpdateTaskRequest
		current  Task
		wantSet  bool
		wantVal  *int
		wantNoOp bool // expect the request's field passed through untouched
	}{
		{
			name:     "neither field touched",
			req:      UpdateTaskRequest{},
			current:  Task{ScheduledTime: ptr("09:00"), EstimatedMinutes: ptr(60)},
			wantNoOp: true,
		},
		{
			name:     "new estimate fits",
			req:      UpdateTaskRequest{EstimatedMinutes: optional.Field[int]{Set: true, Value: ptr(30)}},
			current:  Task{ScheduledTime: ptr("09:00"), EstimatedMinutes: ptr(60)},
			wantNoOp: true,
		},
		{
			name:    "new estimate would cross midnight against the stored time",
			req:     UpdateTaskRequest{EstimatedMinutes: optional.Field[int]{Set: true, Value: ptr(120)}},
			current: Task{ScheduledTime: ptr("23:30"), EstimatedMinutes: ptr(10)},
			wantSet: true, wantVal: ptr(29),
		},
		{
			name:    "moving the time late clamps the untouched stored estimate",
			req:     UpdateTaskRequest{ScheduledTime: optional.Field[string]{Set: true, Value: ptr("23:30")}},
			current: Task{ScheduledTime: ptr("09:00"), EstimatedMinutes: ptr(60)},
			wantSet: true, wantVal: ptr(29),
		},
		{
			name:     "clearing the time removes the ceiling",
			req:      UpdateTaskRequest{ScheduledTime: optional.Field[string]{Set: true, Value: nil}},
			current:  Task{ScheduledTime: ptr("23:30"), EstimatedMinutes: ptr(600)},
			wantNoOp: true,
		},
		{
			name:     "no stored estimate leaves nothing to clamp",
			req:      UpdateTaskRequest{ScheduledTime: optional.Field[string]{Set: true, Value: ptr("23:45")}},
			current:  Task{ScheduledTime: ptr("09:00")},
			wantNoOp: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clampUpdateEstimate(tt.req, tt.current)

			if tt.wantNoOp {
				if got.Set != tt.req.EstimatedMinutes.Set {
					t.Fatalf("Set = %v, want passthrough %v", got.Set, tt.req.EstimatedMinutes.Set)
				}
				return
			}
			if got.Set != tt.wantSet {
				t.Fatalf("Set = %v, want %v", got.Set, tt.wantSet)
			}
			if got.Value == nil {
				t.Fatal("Value = nil, want a clamped estimate")
			}
			if *got.Value != *tt.wantVal {
				t.Errorf("Value = %d, want %d", *got.Value, *tt.wantVal)
			}
		})
	}
}
