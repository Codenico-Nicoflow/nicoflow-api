package task

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// ── mock repository ───────────────────────────────────────────────────────────

type mockRepo struct {
	listByProject    func(ctx context.Context, userID, projectID string, f ListTasksFilter) ([]Task, error)
	getByID          func(ctx context.Context, userID, id string) (*Task, error)
	create           func(ctx context.Context, t Task) (Task, error)
	update           func(ctx context.Context, userID, id string, req UpdateTaskRequest, ca completedAtChange) (Task, error)
	deleteFn         func(ctx context.Context, userID, id string) error
	projectOwned     func(ctx context.Context, userID, projectID string) (bool, error)
	countActiveInbox func(ctx context.Context, userID, projectID string) (int, error)
	nextDisplayOrder func(ctx context.Context, userID, projectID string) (int, error)
	updateSchedule   func(ctx context.Context, userID, id string, scheduledFor *string, rollsOver *bool) (Task, error)
	repack           func(ctx context.Context, userID, id string, targetOrder int) (Task, error)
	listActiveInbox  func(ctx context.Context, userID string) ([]Task, error)
}

func (m *mockRepo) ListByProject(ctx context.Context, userID, projectID string, f ListTasksFilter) ([]Task, error) {
	return m.listByProject(ctx, userID, projectID, f)
}
func (m *mockRepo) GetByID(ctx context.Context, userID, id string) (*Task, error) {
	return m.getByID(ctx, userID, id)
}
func (m *mockRepo) Create(ctx context.Context, t Task) (Task, error) { return m.create(ctx, t) }
func (m *mockRepo) Update(ctx context.Context, userID, id string, req UpdateTaskRequest, ca completedAtChange) (Task, error) {
	return m.update(ctx, userID, id, req, ca)
}
func (m *mockRepo) Delete(ctx context.Context, userID, id string) error {
	return m.deleteFn(ctx, userID, id)
}
func (m *mockRepo) ProjectOwned(ctx context.Context, userID, projectID string) (bool, error) {
	return m.projectOwned(ctx, userID, projectID)
}
func (m *mockRepo) CountActiveInbox(ctx context.Context, userID, projectID string) (int, error) {
	return m.countActiveInbox(ctx, userID, projectID)
}
func (m *mockRepo) NextDisplayOrder(ctx context.Context, userID, projectID string) (int, error) {
	return m.nextDisplayOrder(ctx, userID, projectID)
}
func (m *mockRepo) UpdateSchedule(ctx context.Context, userID, id string, scheduledFor *string, rollsOver *bool) (Task, error) {
	return m.updateSchedule(ctx, userID, id, scheduledFor, rollsOver)
}
func (m *mockRepo) Repack(ctx context.Context, userID, id string, targetOrder int) (Task, error) {
	return m.repack(ctx, userID, id, targetOrder)
}
func (m *mockRepo) ListActiveInboxByUser(ctx context.Context, userID string) ([]Task, error) {
	return m.listActiveInbox(ctx, userID)
}

func appErr(err error) *apperror.AppError {
	var ae *apperror.AppError
	if errors.As(err, &ae) {
		return ae
	}
	return nil
}

func ptr[T any](v T) *T { return &v }

// ── Create ────────────────────────────────────────────────────────────────────

func TestService_Create(t *testing.T) {
	tests := []struct {
		name       string
		plan       string
		req        CreateTaskRequest
		owned      bool
		count      int
		wantErr    bool
		wantCode   string
		wantStatus string // expected default status on the persisted task
		wantDone   bool   // expect completedAt set
	}{
		{
			name:       "title-only applies defaults",
			plan:       "free",
			req:        CreateTaskRequest{Title: "Buy milk"},
			owned:      true,
			count:      0,
			wantStatus: "inbox",
		},
		{
			name:     "empty title rejected",
			plan:     "free",
			req:      CreateTaskRequest{Title: "   "},
			owned:    true,
			wantErr:  true,
			wantCode: apperror.ErrInvalidInput,
		},
		{
			name:     "invalid status rejected",
			plan:     "free",
			req:      CreateTaskRequest{Title: "x", Status: "bogus"},
			owned:    true,
			wantErr:  true,
			wantCode: apperror.ErrInvalidStatus,
		},
		{
			name:     "invalid priority rejected",
			plan:     "free",
			req:      CreateTaskRequest{Title: "x", Priority: "urgent"},
			owned:    true,
			wantErr:  true,
			wantCode: apperror.ErrInvalidPriority,
		},
		{
			name:     "project not owned -> 404",
			plan:     "free",
			req:      CreateTaskRequest{Title: "x"},
			owned:    false,
			wantErr:  true,
			wantCode: apperror.ErrProjectNotFound,
		},
		{
			name:     "free plan limit at 50 active/inbox",
			plan:     "free",
			req:      CreateTaskRequest{Title: "x", Status: "active"},
			owned:    true,
			count:    50,
			wantErr:  true,
			wantCode: apperror.ErrPlanLimitExceeded,
		},
		{
			name:       "pro plan ignores limit",
			plan:       "pro",
			req:        CreateTaskRequest{Title: "x", Status: "active"},
			owned:      true,
			count:      999,
			wantStatus: "active",
		},
		{
			name:       "someday does not count toward limit",
			plan:       "free",
			req:        CreateTaskRequest{Title: "x", Status: "someday"},
			owned:      true,
			count:      50,
			wantStatus: "someday",
		},
		{
			name:       "status=done sets completedAt",
			plan:       "free",
			req:        CreateTaskRequest{Title: "x", Status: "done"},
			owned:      true,
			wantStatus: "done",
			wantDone:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var persisted Task
			repo := &mockRepo{
				projectOwned:     func(_ context.Context, _, _ string) (bool, error) { return tt.owned, nil },
				countActiveInbox: func(_ context.Context, _, _ string) (int, error) { return tt.count, nil },
				nextDisplayOrder: func(_ context.Context, _, _ string) (int, error) { return 3, nil },
				create: func(_ context.Context, task Task) (Task, error) {
					persisted = task
					return task, nil
				},
			}
			svc := NewService(repo)

			view, err := svc.Create(context.Background(), "u1", "p1", tt.plan, tt.req)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				if ae := appErr(err); ae == nil || ae.Code != tt.wantCode {
					t.Fatalf("want code %s, got %+v", tt.wantCode, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertCreated(t, persisted, view, tt.wantStatus, tt.wantDone)
		})
	}
}

func assertCreated(t *testing.T, persisted Task, view TaskView, wantStatus string, wantDone bool) {
	t.Helper()
	if persisted.Status != wantStatus {
		t.Errorf("status = %q, want %q", persisted.Status, wantStatus)
	}
	if persisted.Priority != "medium" || persisted.Energy != "medium" {
		t.Errorf("defaults wrong: priority=%q energy=%q", persisted.Priority, persisted.Energy)
	}
	if !persisted.RollsOver {
		t.Errorf("rollsOver should default true")
	}
	if persisted.DisplayOrder != 3 {
		t.Errorf("displayOrder = %d, want 3", persisted.DisplayOrder)
	}
	if wantDone != (persisted.CompletedAt != nil) {
		t.Errorf("completedAt presence = %v, want %v", persisted.CompletedAt != nil, wantDone)
	}
	if view.ID != persisted.ID {
		t.Errorf("view ID mismatch")
	}
}

// ── completedAt transitions on Update ──────────────────────────────────────────

func TestService_Update_CompletedAtTransitions(t *testing.T) {
	tests := []struct {
		name          string
		currentStatus string
		newStatus     *string
		wantChange    completedAtChange
	}{
		{"active -> done sets now", "active", ptr("done"), completedAtSetNow},
		{"done -> active clears", "done", ptr("active"), completedAtClear},
		{"active -> someday keeps", "active", ptr("someday"), completedAtKeep},
		{"no status change keeps", "active", nil, completedAtKeep},
		{"done -> done keeps", "done", ptr("done"), completedAtKeep},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotChange completedAtChange
			repo := &mockRepo{
				getByID: func(_ context.Context, _, _ string) (*Task, error) {
					return &Task{ID: "t1", ProjectID: "p1", Status: tt.currentStatus}, nil
				},
				countActiveInbox: func(_ context.Context, _, _ string) (int, error) { return 0, nil },
				update: func(_ context.Context, _, _ string, _ UpdateTaskRequest, ca completedAtChange) (Task, error) {
					gotChange = ca
					return Task{ID: "t1", Status: "x"}, nil
				},
			}
			svc := NewService(repo)

			_, err := svc.Update(context.Background(), "u1", "t1", "pro", UpdateTaskRequest{Status: tt.newStatus})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotChange != tt.wantChange {
				t.Errorf("completedAt change = %d, want %d", gotChange, tt.wantChange)
			}
		})
	}
}

// ── Update plan limit on move into active/inbox ────────────────────────────────

func TestService_Update_PlanLimitOnMoveIntoActive(t *testing.T) {
	repo := &mockRepo{
		getByID: func(_ context.Context, _, _ string) (*Task, error) {
			return &Task{ID: "t1", ProjectID: "p1", Status: "someday"}, nil
		},
		countActiveInbox: func(_ context.Context, _, _ string) (int, error) { return 50, nil },
		update: func(_ context.Context, _, _ string, _ UpdateTaskRequest, _ completedAtChange) (Task, error) {
			t.Fatal("update should not be called when limit exceeded")
			return Task{}, nil
		},
	}
	svc := NewService(repo)

	_, err := svc.Update(context.Background(), "u1", "t1", "free", UpdateTaskRequest{Status: ptr("active")})
	if ae := appErr(err); ae == nil || ae.Code != apperror.ErrPlanLimitExceeded {
		t.Fatalf("want PLAN_LIMIT_EXCEEDED, got %+v", err)
	}
	if ae := appErr(err); ae.Status != http.StatusForbidden {
		t.Errorf("want 403, got %d", ae.Status)
	}
}

// ── List requires project ownership ────────────────────────────────────────────

func TestService_ListByProject_NotOwned(t *testing.T) {
	repo := &mockRepo{
		projectOwned: func(_ context.Context, _, _ string) (bool, error) { return false, nil },
	}
	svc := NewService(repo)

	_, err := svc.ListByProject(context.Background(), "u1", "p1", ListTasksFilter{})
	if ae := appErr(err); ae == nil || ae.Code != apperror.ErrProjectNotFound {
		t.Fatalf("want PROJECT_NOT_FOUND, got %+v", err)
	}
}

// ── quick actions ──────────────────────────────────────────────────────────────

func TestService_SetStatus_EmptyRejected(t *testing.T) {
	svc := NewService(&mockRepo{})
	_, err := svc.SetStatus(context.Background(), "u1", "t1", "pro", "")
	if ae := appErr(err); ae == nil || ae.Code != apperror.ErrInvalidInput {
		t.Fatalf("want INVALID_INPUT, got %+v", err)
	}
}

func TestService_SetStatus_DelegatesToUpdate(t *testing.T) {
	var gotChange completedAtChange
	repo := &mockRepo{
		getByID: func(_ context.Context, _, _ string) (*Task, error) {
			return &Task{ID: "t1", ProjectID: "p1", Status: "active"}, nil
		},
		countActiveInbox: func(_ context.Context, _, _ string) (int, error) { return 0, nil },
		update: func(_ context.Context, _, _ string, _ UpdateTaskRequest, ca completedAtChange) (Task, error) {
			gotChange = ca
			return Task{ID: "t1", Status: "done"}, nil
		},
	}
	v, err := NewService(repo).SetStatus(context.Background(), "u1", "t1", "pro", "done")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotChange != completedAtSetNow {
		t.Errorf("status=done should set completedAt, got change %d", gotChange)
	}
	if v.Status != "done" {
		t.Errorf("status = %q", v.Status)
	}
}

func TestService_Schedule(t *testing.T) {
	tests := []struct {
		name     string
		date     *string
		wantErr  bool
		wantCode string
	}{
		{name: "valid ISO date", date: ptr("2026-07-01")},
		{name: "null unschedules", date: nil},
		{name: "bad date rejected", date: ptr("July 1st"), wantErr: true, wantCode: apperror.ErrInvalidDate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockRepo{
				updateSchedule: func(_ context.Context, _, _ string, sf *string, _ *bool) (Task, error) {
					return Task{ID: "t1", ScheduledFor: sf}, nil
				},
			}
			_, err := NewService(repo).Schedule(context.Background(), "u1", "t1", ScheduleRequest{ScheduledFor: tt.date})
			if tt.wantErr {
				if ae := appErr(err); ae == nil || ae.Code != tt.wantCode {
					t.Fatalf("want %s, got %+v", tt.wantCode, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestService_ReorderOne_NegativeRejected(t *testing.T) {
	svc := NewService(&mockRepo{})
	_, err := svc.ReorderOne(context.Background(), "u1", "t1", -1)
	if ae := appErr(err); ae == nil || ae.Code != apperror.ErrInvalidInput {
		t.Fatalf("want INVALID_INPUT, got %+v", err)
	}
}

func TestService_Focus(t *testing.T) {
	now := func() time.Time { return time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC) }

	t.Run("invalid energy rejected", func(t *testing.T) {
		svc := NewServiceWithClock(&mockRepo{}, now)
		_, err := svc.Focus(context.Background(), "u1", FocusParams{Energy: "bogus"})
		if ae := appErr(err); ae == nil || ae.Code != apperror.ErrInvalidInput {
			t.Fatalf("want INVALID_INPUT, got %+v", err)
		}
	})

	t.Run("ranks + clamps to default limit", func(t *testing.T) {
		candidates := make([]Task, 8)
		for i := range candidates {
			candidates[i] = Task{ID: string(rune('a' + i)), Energy: "low", Priority: "low", Status: "active"}
		}
		repo := &mockRepo{
			listActiveInbox: func(_ context.Context, _ string) ([]Task, error) { return candidates, nil },
		}
		svc := NewServiceWithClock(repo, now)
		resp, err := svc.Focus(context.Background(), "u1", FocusParams{}) // no limit → default 5
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(resp.Items) != defaultFocusLimit {
			t.Errorf("len = %d, want %d", len(resp.Items), defaultFocusLimit)
		}
	})

	t.Run("excludes nothing extra — repo provides candidate set", func(t *testing.T) {
		var gotUser string
		repo := &mockRepo{
			listActiveInbox: func(_ context.Context, userID string) ([]Task, error) {
				gotUser = userID
				return nil, nil
			},
		}
		svc := NewServiceWithClock(repo, now)
		resp, err := svc.Focus(context.Background(), "u9", FocusParams{Limit: 999})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if gotUser != "u9" {
			t.Errorf("candidate query scoped to %q, want u9", gotUser)
		}
		if len(resp.Items) != 0 {
			t.Errorf("empty candidates → empty result, got %d", len(resp.Items))
		}
	})
}
