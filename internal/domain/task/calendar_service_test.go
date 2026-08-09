package task

import (
	"context"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/optional"
)

// timedRepo is a mockRepo wired for the timed-scheduling write paths, capturing
// what would be persisted so the tests can assert the gate blocked the write
// rather than merely returning an error after it.
func timedRepo(stored Task, persisted *Task) *mockRepo {
	return &mockRepo{
		projectOwned:     func(context.Context, string, string) (bool, error) { return true, nil },
		countActive:      func(context.Context, string, string) (int, error) { return 0, nil },
		nextDisplayOrder: func(context.Context, string, string) (int, error) { return 0, nil },
		getByID: func(context.Context, string, string) (*Task, error) {
			t := stored
			return &t, nil
		},
		create: func(_ context.Context, t Task) (Task, error) {
			*persisted = t
			return t, nil
		},
		update: func(_ context.Context, _, _ string, req UpdateTaskRequest, _ completedAtChange) (Task, error) {
			out := stored
			if req.ScheduledTime.Set {
				out.ScheduledTime = req.ScheduledTime.Value
			}
			if req.EstimatedMinutes.Set {
				out.EstimatedMinutes = req.EstimatedMinutes.Value
			}
			*persisted = out
			return out, nil
		},
		updateSchedule: func(_ context.Context, _, _ string, sf, st *string, _ *bool) (Task, error) {
			out := stored
			out.ScheduledFor, out.ScheduledTime = sf, st
			*persisted = out
			return out, nil
		},
	}
}

// The gate must hold on every write path, or the frontend picks the weakest one.
func TestService_TimedScheduling_PlanGate(t *testing.T) {
	tests := []struct {
		name     string
		plan     string
		call     func(svc Service) error
		wantErr  bool
		wantCode string
	}{
		{
			name: "free cannot set a time on create",
			plan: "free",
			call: func(svc Service) error {
				_, err := svc.Create(context.Background(), "u1", "p1", "free",
					CreateTaskRequest{Title: "standup", ScheduledFor: ptr("2026-08-01"), ScheduledTime: ptr("09:00")})
				return err
			},
			wantErr: true, wantCode: apperror.ErrPlanLimitExceeded,
		},
		{
			name: "pro can set a time on create",
			plan: "pro",
			call: func(svc Service) error {
				_, err := svc.Create(context.Background(), "u1", "p1", "pro",
					CreateTaskRequest{Title: "standup", ScheduledFor: ptr("2026-08-01"), ScheduledTime: ptr("09:00")})
				return err
			},
		},
		{
			name: "free can create without a time",
			plan: "free",
			call: func(svc Service) error {
				_, err := svc.Create(context.Background(), "u1", "p1", "free",
					CreateTaskRequest{Title: "standup", ScheduledFor: ptr("2026-08-01")})
				return err
			},
		},
		{
			name: "free cannot set a time on update",
			plan: "free",
			call: func(svc Service) error {
				_, err := svc.Update(context.Background(), "u1", "t1", "free",
					UpdateTaskRequest{ScheduledTime: optional.Field[string]{Set: true, Value: ptr("09:00")}})
				return err
			},
			wantErr: true, wantCode: apperror.ErrPlanLimitExceeded,
		},
		{
			name: "free CAN clear a time on update — a downgraded user is never trapped",
			plan: "free",
			call: func(svc Service) error {
				_, err := svc.Update(context.Background(), "u1", "t1", "free",
					UpdateTaskRequest{ScheduledTime: optional.Field[string]{Set: true, Value: nil}})
				return err
			},
		},
		{
			name: "free update untouched by the gate when the field is absent",
			plan: "free",
			call: func(svc Service) error {
				_, err := svc.Update(context.Background(), "u1", "t1", "free",
					UpdateTaskRequest{Title: ptr("renamed")})
				return err
			},
		},
		{
			name: "free cannot set a time on schedule",
			plan: "free",
			call: func(svc Service) error {
				_, err := svc.Schedule(context.Background(), "u1", "t1", "free",
					ScheduleRequest{ScheduledFor: ptr("2026-08-01"), ScheduledTime: ptr("09:00")})
				return err
			},
			wantErr: true, wantCode: apperror.ErrPlanLimitExceeded,
		},
		{
			name: "free can schedule a date with no time",
			plan: "free",
			call: func(svc Service) error {
				_, err := svc.Schedule(context.Background(), "u1", "t1", "free",
					ScheduleRequest{ScheduledFor: ptr("2026-08-01")})
				return err
			},
		},
		{
			name: "pro can schedule with a time",
			plan: "pro",
			call: func(svc Service) error {
				_, err := svc.Schedule(context.Background(), "u1", "t1", "pro",
					ScheduleRequest{ScheduledFor: ptr("2026-08-01"), ScheduledTime: ptr("09:00")})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var persisted Task
			stored := Task{ID: "t1", UserID: "u1", ProjectID: "p1", Title: "x", Status: "active",
				ScheduledFor: ptr("2026-08-01"), ScheduledTime: ptr("10:00")}

			err := tt.call(NewService(timedRepo(stored, &persisted), nil, nil))

			if tt.wantErr {
				ae := appErr(err)
				if ae == nil || ae.Code != tt.wantCode {
					t.Fatalf("want %s, got %+v", tt.wantCode, err)
				}
				if ae.Status != 403 {
					t.Errorf("status = %d, want 403", ae.Status)
				}
				// The gate must block BEFORE the write, not just shape the response.
				if persisted.ID != "" {
					t.Errorf("gated write still persisted %+v", persisted)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

// Validation runs on every write path too — a non-snapped time must never reach
// the column, whatever the plan.
func TestService_TimedScheduling_Validation(t *testing.T) {
	tests := []struct {
		name string
		call func(svc Service) error
	}{
		{
			name: "create rejects a non-snapped time",
			call: func(svc Service) error {
				_, err := svc.Create(context.Background(), "u1", "p1", "pro",
					CreateTaskRequest{Title: "x", ScheduledFor: ptr("2026-08-01"), ScheduledTime: ptr("09:07")})
				return err
			},
		},
		{
			name: "update rejects a non-snapped time",
			call: func(svc Service) error {
				_, err := svc.Update(context.Background(), "u1", "t1", "pro",
					UpdateTaskRequest{ScheduledTime: optional.Field[string]{Set: true, Value: ptr("09:07")}})
				return err
			},
		},
		{
			name: "schedule rejects a non-snapped time",
			call: func(svc Service) error {
				_, err := svc.Schedule(context.Background(), "u1", "t1", "pro",
					ScheduleRequest{ScheduledFor: ptr("2026-08-01"), ScheduledTime: ptr("09:07")})
				return err
			},
		},
		{
			name: "schedule rejects a time with no date to land on",
			call: func(svc Service) error {
				_, err := svc.Schedule(context.Background(), "u1", "t1", "pro",
					ScheduleRequest{ScheduledTime: ptr("09:00")})
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var persisted Task
			stored := Task{ID: "t1", UserID: "u1", ProjectID: "p1", Title: "x", Status: "active"}

			err := tt.call(NewService(timedRepo(stored, &persisted), nil, nil))

			ae := appErr(err)
			if ae == nil || ae.Code != apperror.ErrInvalidInput {
				t.Fatalf("want INVALID_INPUT, got %+v", err)
			}
			if ae.Status != 422 {
				t.Errorf("status = %d, want 422", ae.Status)
			}
			if persisted.ID != "" {
				t.Errorf("rejected write still persisted %+v", persisted)
			}
		})
	}
}

// The clamp is a write-time invariant: what lands in the column can never
// describe a task that runs past midnight.
func TestService_TimedScheduling_ClampsOnWrite(t *testing.T) {
	t.Run("create clamps the estimate", func(t *testing.T) {
		var persisted Task
		repo := timedRepo(Task{}, &persisted)

		_, err := NewService(repo, nil, nil).Create(context.Background(), "u1", "p1", "pro",
			CreateTaskRequest{Title: "late", ScheduledFor: ptr("2026-08-01"),
				ScheduledTime: ptr("23:30"), EstimatedMinutes: ptr(60)})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if persisted.EstimatedMinutes == nil || *persisted.EstimatedMinutes != 29 {
			t.Errorf("persisted estimate = %v, want 29", persisted.EstimatedMinutes)
		}
	})

	t.Run("update clamps against the stored time", func(t *testing.T) {
		var persisted Task
		stored := Task{ID: "t1", UserID: "u1", ProjectID: "p1", Status: "active", ScheduledTime: ptr("23:30")}
		repo := timedRepo(stored, &persisted)

		_, err := NewService(repo, nil, nil).Update(context.Background(), "u1", "t1", "pro",
			UpdateTaskRequest{EstimatedMinutes: optional.Field[int]{Set: true, Value: ptr(120)}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if persisted.EstimatedMinutes == nil || *persisted.EstimatedMinutes != 29 {
			t.Errorf("persisted estimate = %v, want 29", persisted.EstimatedMinutes)
		}
	})
}

// A downgraded user's stored time must survive and keep being returned —
// downgrade is graceful, never destructive.
func TestService_TimedScheduling_DowngradePreservesStoredTime(t *testing.T) {
	stored := Task{ID: "t1", UserID: "u1", ProjectID: "p1", Title: "x", Status: "active",
		ScheduledFor: ptr("2026-08-01"), ScheduledTime: ptr("09:00")}
	var persisted Task

	view, err := NewService(timedRepo(stored, &persisted), nil, nil).Get(context.Background(), "u1", "t1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if view.ScheduledTime == nil || *view.ScheduledTime != "09:00" {
		t.Errorf("scheduledTime = %v, want 09:00 preserved after downgrade", view.ScheduledTime)
	}
}

func TestService_ListByDateRange(t *testing.T) {
	t.Run("passes the validated window to the repo and maps the view", func(t *testing.T) {
		var gotFrom, gotTo string
		repo := &mockRepo{
			listByDateRange: func(_ context.Context, _, from, to string) ([]Task, error) {
				gotFrom, gotTo = from, to
				return []Task{
					{ID: "a", ScheduledFor: ptr("2026-08-01")},
					{ID: "b", ScheduledFor: ptr("2026-08-01"), ScheduledTime: ptr("09:00")},
				}, nil
			},
		}

		resp, err := NewService(repo, nil, nil).ListByDateRange(context.Background(), "u1", "2026-08-01", "2026-08-31")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotFrom != "2026-08-01" || gotTo != "2026-08-31" {
			t.Errorf("repo window = %s..%s", gotFrom, gotTo)
		}
		if len(resp.Items) != 2 {
			t.Fatalf("items = %d, want 2", len(resp.Items))
		}
		if resp.Items[0].ScheduledTime != nil {
			t.Errorf("all-day task leaked a time: %v", resp.Items[0].ScheduledTime)
		}
		if resp.Items[1].ScheduledTime == nil || *resp.Items[1].ScheduledTime != "09:00" {
			t.Errorf("timed task scheduledTime = %v, want 09:00", resp.Items[1].ScheduledTime)
		}
	})

	t.Run("an empty range serialises as an empty list, never null", func(t *testing.T) {
		repo := &mockRepo{
			listByDateRange: func(context.Context, string, string, string) ([]Task, error) { return nil, nil },
		}

		resp, err := NewService(repo, nil, nil).ListByDateRange(context.Background(), "u1", "2026-08-01", "2026-08-31")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if resp.Items == nil {
			t.Error("Items = nil, want an empty slice")
		}
	})

	t.Run("an invalid window never reaches the repo", func(t *testing.T) {
		called := false
		repo := &mockRepo{
			listByDateRange: func(context.Context, string, string, string) ([]Task, error) {
				called = true
				return nil, nil
			},
		}

		_, err := NewService(repo, nil, nil).ListByDateRange(context.Background(), "u1", "2026-01-01", "2026-12-31")
		if ae := appErr(err); ae == nil || ae.Code != apperror.ErrInvalidInput {
			t.Fatalf("want INVALID_INPUT, got %+v", err)
		}
		if called {
			t.Error("repo was queried despite an invalid window")
		}
	})
}
