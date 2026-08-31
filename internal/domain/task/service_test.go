package task

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/optional"
)

// ── mock repository ───────────────────────────────────────────────────────────

type mockRepo struct {
	listByProject    func(ctx context.Context, userID, projectID string, f ListTasksFilter) ([]Task, string, error)
	getByID          func(ctx context.Context, userID, id string) (*Task, error)
	create           func(ctx context.Context, t Task) (Task, error)
	update           func(ctx context.Context, userID, id string, req UpdateTaskRequest, ca completedAtChange) (Task, error)
	deleteFn         func(ctx context.Context, userID, id string) error
	projectOwned     func(ctx context.Context, userID, projectID string) (bool, error)
	countActive      func(ctx context.Context, userID, projectID string) (int, error)
	countNonTerminal func(ctx context.Context, userID, projectID string) (int, error)
	nextDisplayOrder func(ctx context.Context, userID, projectID string) (int, error)
	updateSchedule   func(ctx context.Context, userID, id string, scheduledFor, scheduledTime *string, rollsOver *bool) (Task, error)
	repack           func(ctx context.Context, userID, id string, targetOrder int) (Task, error)
	listActiveByUser func(ctx context.Context, userID string) ([]Task, error)
	listByDateRange  func(ctx context.Context, userID, from, to string) ([]Task, error)
	existsByID       func(ctx context.Context, id string) (bool, error)
	isOpenable       func(ctx context.Context, userID, id string) (bool, error)
	listForUser      func(ctx context.Context, userID string, f UserListFilter) ([]Task, error)
	markMissed       func(ctx context.Context, userID, id string) (*Task, error)
	markSkipped      func(ctx context.Context, userID, id string) (*Task, error)
}

func (m *mockRepo) ListByProject(ctx context.Context, userID, projectID string, f ListTasksFilter) ([]Task, string, error) {
	return m.listByProject(ctx, userID, projectID, f)
}
func (m *mockRepo) GetByID(ctx context.Context, userID, id string) (*Task, error) {
	if m.getByID == nil {
		return &Task{ID: id, UserID: userID, Status: statusActive}, nil
	}
	return m.getByID(ctx, userID, id)
}
func (m *mockRepo) Create(ctx context.Context, t Task) (Task, error) { return m.create(ctx, t) }
func (m *mockRepo) MarkMissed(ctx context.Context, userID, id string) (*Task, error) {
	if m.markMissed == nil {
		return nil, nil
	}
	return m.markMissed(ctx, userID, id)
}
func (m *mockRepo) MarkSkipped(ctx context.Context, userID, id string) (*Task, error) {
	if m.markSkipped == nil {
		return nil, nil
	}
	return m.markSkipped(ctx, userID, id)
}
func (m *mockRepo) Update(ctx context.Context, userID, id string, req UpdateTaskRequest, ca completedAtChange) (Task, error) {
	return m.update(ctx, userID, id, req, ca)
}
func (m *mockRepo) Delete(ctx context.Context, userID, id string) error {
	return m.deleteFn(ctx, userID, id)
}
func (m *mockRepo) ProjectOwned(ctx context.Context, userID, projectID string) (bool, error) {
	return m.projectOwned(ctx, userID, projectID)
}
func (m *mockRepo) CountActive(ctx context.Context, userID, projectID string) (int, error) {
	return m.countActive(ctx, userID, projectID)
}
func (m *mockRepo) CountNonTerminalByProject(ctx context.Context, userID, projectID string) (int, error) {
	if m.countNonTerminal == nil {
		return 0, nil
	}
	return m.countNonTerminal(ctx, userID, projectID)
}
func (m *mockRepo) NextDisplayOrder(ctx context.Context, userID, projectID string) (int, error) {
	return m.nextDisplayOrder(ctx, userID, projectID)
}
func (m *mockRepo) UpdateSchedule(ctx context.Context, userID, id string, scheduledFor, scheduledTime *string, rollsOver *bool) (Task, error) {
	return m.updateSchedule(ctx, userID, id, scheduledFor, scheduledTime, rollsOver)
}
func (m *mockRepo) ListByDateRange(ctx context.Context, userID, from, to string) ([]Task, error) {
	if m.listByDateRange == nil {
		return nil, nil
	}
	return m.listByDateRange(ctx, userID, from, to)
}
func (m *mockRepo) Repack(ctx context.Context, userID, id string, targetOrder int) (Task, error) {
	return m.repack(ctx, userID, id, targetOrder)
}
func (m *mockRepo) ListActiveByUser(ctx context.Context, userID string) ([]Task, error) {
	return m.listActiveByUser(ctx, userID)
}
func (m *mockRepo) ExistsByID(ctx context.Context, id string) (bool, error) {
	if m.existsByID != nil {
		return m.existsByID(ctx, id)
	}
	return false, nil
}
func (m *mockRepo) IsOpenable(ctx context.Context, userID, id string) (bool, error) {
	if m.isOpenable != nil {
		return m.isOpenable(ctx, userID, id)
	}
	return false, nil
}
func (m *mockRepo) ListForUser(ctx context.Context, userID string, f UserListFilter) ([]Task, error) {
	if m.listForUser != nil {
		return m.listForUser(ctx, userID, f)
	}
	return nil, nil
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
			wantStatus: "active",
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
			name:     "non-date scheduledFor rejected",
			plan:     "free",
			req:      CreateTaskRequest{Title: "x", ScheduledFor: ptr("low")},
			owned:    true,
			wantErr:  true,
			wantCode: apperror.ErrInvalidDate,
		},
		{
			name:       "ISO scheduledFor accepted",
			plan:       "free",
			req:        CreateTaskRequest{Title: "x", ScheduledFor: ptr("2026-07-15")},
			owned:      true,
			wantStatus: "active",
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
			name:     "free plan limit at 50 active",
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
				countActive:      func(_ context.Context, _, _ string) (int, error) { return tt.count, nil },
				nextDisplayOrder: func(_ context.Context, _, _ string) (int, error) { return 3, nil },
				create: func(_ context.Context, task Task) (Task, error) {
					persisted = task
					return task, nil
				},
			}
			svc := NewService(repo, nil, nil)

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
		{"active -> cancelled keeps", "active", ptr("cancelled"), completedAtKeep},
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
				countActive: func(_ context.Context, _, _ string) (int, error) { return 0, nil },
				update: func(_ context.Context, _, _ string, _ UpdateTaskRequest, ca completedAtChange) (Task, error) {
					gotChange = ca
					return Task{ID: "t1", Status: "x"}, nil
				},
			}
			svc := NewService(repo, nil, nil)

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

// ── Update plan limit on move into active ───────────────────────────────────────

func TestService_Update_PlanLimitOnMoveIntoActive(t *testing.T) {
	repo := &mockRepo{
		getByID: func(_ context.Context, _, _ string) (*Task, error) {
			return &Task{ID: "t1", ProjectID: "p1", Status: "cancelled"}, nil
		},
		countActive: func(_ context.Context, _, _ string) (int, error) { return 50, nil },
		update: func(_ context.Context, _, _ string, _ UpdateTaskRequest, _ completedAtChange) (Task, error) {
			t.Fatal("update should not be called when limit exceeded")
			return Task{}, nil
		},
	}
	svc := NewService(repo, nil, nil)

	_, err := svc.Update(context.Background(), "u1", "t1", "free", UpdateTaskRequest{Status: ptr("active")})
	if ae := appErr(err); ae == nil || ae.Code != apperror.ErrPlanLimitExceeded {
		t.Fatalf("want PLAN_LIMIT_EXCEEDED, got %+v", err)
	}
	if ae := appErr(err); ae.Status != http.StatusForbidden {
		t.Errorf("want 403, got %d", ae.Status)
	}
}

// ── Update project reassignment ─────────────────────────────────────────────────

func TestService_Update_ProjectReassignment(t *testing.T) {
	tests := []struct {
		name         string
		req          UpdateTaskRequest
		projectOwned bool
		wantErrCode  string
		wantProject  *string // expected ProjectID passed to repo.Update
	}{
		{
			name:         "reassigns to a project owned by the caller",
			req:          UpdateTaskRequest{ProjectID: ptr("p2")},
			projectOwned: true,
			wantProject:  ptr("p2"),
		},
		{
			name:         "rejects a project owned by another user",
			req:          UpdateTaskRequest{ProjectID: ptr("other-users-project")},
			projectOwned: false,
			wantErrCode:  apperror.ErrProjectNotFound,
		},
		{
			name:         "rejects a nonexistent project",
			req:          UpdateTaskRequest{ProjectID: ptr("does-not-exist")},
			projectOwned: false,
			wantErrCode:  apperror.ErrProjectNotFound,
		},
		{
			name: "other fields still update alongside a reassignment",
			req: UpdateTaskRequest{
				ProjectID: ptr("p2"),
				Title:     ptr("Renamed while moving"),
			},
			projectOwned: true,
			wantProject:  ptr("p2"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotOwnedCheckProject string
			var gotUpdateReq UpdateTaskRequest
			updateCalled := false

			repo := &mockRepo{
				getByID: func(_ context.Context, _, _ string) (*Task, error) {
					return &Task{ID: "t1", ProjectID: "p1", Status: "active"}, nil
				},
				projectOwned: func(_ context.Context, _, projectID string) (bool, error) {
					gotOwnedCheckProject = projectID
					return tt.projectOwned, nil
				},
				countActive: func(_ context.Context, _, _ string) (int, error) { return 0, nil },
				update: func(_ context.Context, _, _ string, req UpdateTaskRequest, _ completedAtChange) (Task, error) {
					updateCalled = true
					gotUpdateReq = req
					return Task{ID: "t1", ProjectID: "p2", Title: "Renamed while moving", Status: "active"}, nil
				},
			}
			svc := NewService(repo, nil, nil)

			_, err := svc.Update(context.Background(), "u1", "t1", "pro", tt.req)

			if tt.wantErrCode != "" {
				ae := appErr(err)
				if ae == nil || ae.Code != tt.wantErrCode {
					t.Fatalf("want %s, got %+v", tt.wantErrCode, err)
				}
				if updateCalled {
					t.Error("repo.Update must not be called when the target project is not owned")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotOwnedCheckProject != *tt.req.ProjectID {
				t.Errorf("ProjectOwned checked %q, want %q", gotOwnedCheckProject, *tt.req.ProjectID)
			}
			if !updateCalled {
				t.Fatal("repo.Update was not called")
			}
			if gotUpdateReq.ProjectID == nil || *gotUpdateReq.ProjectID != *tt.wantProject {
				t.Errorf("repo.Update received ProjectID = %v, want %v", gotUpdateReq.ProjectID, *tt.wantProject)
			}
			if tt.req.Title != nil && (gotUpdateReq.Title == nil || *gotUpdateReq.Title != *tt.req.Title) {
				t.Errorf("repo.Update received Title = %v, want %v", gotUpdateReq.Title, tt.req.Title)
			}
		})
	}
}

func TestService_Update_NoProjectIDSkipsOwnershipCheck(t *testing.T) {
	ownedCheckCalled := false
	repo := &mockRepo{
		getByID: func(_ context.Context, _, _ string) (*Task, error) {
			return &Task{ID: "t1", ProjectID: "p1", Status: "active"}, nil
		},
		projectOwned: func(_ context.Context, _, _ string) (bool, error) {
			ownedCheckCalled = true
			return true, nil
		},
		countActive: func(_ context.Context, _, _ string) (int, error) { return 0, nil },
		update: func(_ context.Context, _, _ string, _ UpdateTaskRequest, _ completedAtChange) (Task, error) {
			return Task{ID: "t1", ProjectID: "p1", Title: "Renamed"}, nil
		},
	}
	svc := NewService(repo, nil, nil)

	_, err := svc.Update(context.Background(), "u1", "t1", "pro", UpdateTaskRequest{Title: ptr("Renamed")})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ownedCheckCalled {
		t.Error("ProjectOwned must not be called when projectId is absent from the PATCH")
	}
}

// ── List requires project ownership ────────────────────────────────────────────

func TestService_ListByProject_NotOwned(t *testing.T) {
	repo := &mockRepo{
		projectOwned: func(_ context.Context, _, _ string) (bool, error) { return false, nil },
	}
	svc := NewService(repo, nil, nil)

	_, err := svc.ListByProject(context.Background(), "u1", "p1", ListTasksFilter{})
	if ae := appErr(err); ae == nil || ae.Code != apperror.ErrProjectNotFound {
		t.Fatalf("want PROJECT_NOT_FOUND, got %+v", err)
	}
}

// ── quick actions ──────────────────────────────────────────────────────────────

func TestService_SetStatus_EmptyRejected(t *testing.T) {
	svc := NewService(&mockRepo{}, nil, nil)
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
		countActive: func(_ context.Context, _, _ string) (int, error) { return 0, nil },
		update: func(_ context.Context, _, _ string, _ UpdateTaskRequest, ca completedAtChange) (Task, error) {
			gotChange = ca
			return Task{ID: "t1", Status: "done"}, nil
		},
	}
	v, err := NewService(repo, nil, nil).SetStatus(context.Background(), "u1", "t1", "pro", "done")
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
				updateSchedule: func(_ context.Context, _, _ string, sf, st *string, _ *bool) (Task, error) {
					return Task{ID: "t1", ScheduledFor: sf, ScheduledTime: st}, nil
				},
			}
			_, err := NewService(repo, nil, nil).Schedule(context.Background(), "u1", "t1", "pro", ScheduleRequest{ScheduledFor: tt.date})
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

func TestService_Update_ScheduledForValidation(t *testing.T) {
	tests := []struct {
		name     string
		sf       optional.Field[string]
		wantErr  bool
		wantCode string
	}{
		{name: "ISO date accepted", sf: optional.Field[string]{Set: true, Value: ptr("2026-07-15")}},
		{name: "clear (explicit null) accepted", sf: optional.Field[string]{Set: true, Value: nil}},
		{name: "absent accepted", sf: optional.Field[string]{}},
		{name: "non-date rejected", sf: optional.Field[string]{Set: true, Value: ptr("low")}, wantErr: true, wantCode: apperror.ErrInvalidDate},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockRepo{
				getByID: func(_ context.Context, _, _ string) (*Task, error) {
					return &Task{ID: "t1", Status: "active"}, nil
				},
				update: func(_ context.Context, _, _ string, _ UpdateTaskRequest, _ completedAtChange) (Task, error) {
					return Task{ID: "t1"}, nil
				},
			}
			_, err := NewService(repo, nil, nil).Update(context.Background(), "u1", "t1", "pro", UpdateTaskRequest{ScheduledFor: tt.sf})
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
	svc := NewService(&mockRepo{}, nil, nil)
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
			listActiveByUser: func(_ context.Context, _ string) ([]Task, error) { return candidates, nil },
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
			listActiveByUser: func(_ context.Context, userID string) ([]Task, error) {
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

// ── attachment cleanup on delete (NIC-1651) ──────────────────────────────────

type fakeCleaner struct {
	calls []cleanerCall
	err   error
}

type cleanerCall struct{ userID, ownerType, ownerID string }

func (f *fakeCleaner) DeleteAllForOwner(_ context.Context, userID, ownerType, ownerID string) error {
	f.calls = append(f.calls, cleanerCall{userID, ownerType, ownerID})
	return f.err
}

func TestService_Delete_InvokesCleaner(t *testing.T) {
	repo := &mockRepo{deleteFn: func(_ context.Context, _, _ string) error { return nil }}
	cleaner := &fakeCleaner{}
	svc := NewService(repo, nil, nil).WithCleaner(cleaner)

	if err := svc.Delete(context.Background(), "u1", "t1"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(cleaner.calls) != 1 {
		t.Fatalf("cleaner called %d times, want 1", len(cleaner.calls))
	}
	got := cleaner.calls[0]
	if got != (cleanerCall{"u1", ownerTypeTask, "t1"}) {
		t.Fatalf("cleaner called with %+v, want {u1 task t1}", got)
	}
}

func TestService_Delete_CleanerErrorDoesNotFailDelete(t *testing.T) {
	repo := &mockRepo{deleteFn: func(_ context.Context, _, _ string) error { return nil }}
	cleaner := &fakeCleaner{err: errors.New("s3 unreachable")}
	svc := NewService(repo, nil, nil).WithCleaner(cleaner)

	if err := svc.Delete(context.Background(), "u1", "t1"); err != nil {
		t.Fatalf("cleaner failure must not fail delete, got: %v", err)
	}
}

func TestService_Delete_RepoErrorSkipsCleaner(t *testing.T) {
	repo := &mockRepo{deleteFn: func(_ context.Context, _, _ string) error {
		return apperror.New(http.StatusNotFound, apperror.ErrTaskNotFound, "not found")
	}}
	cleaner := &fakeCleaner{}
	svc := NewService(repo, nil, nil).WithCleaner(cleaner)

	if err := svc.Delete(context.Background(), "u1", "t1"); err == nil {
		t.Fatal("expected delete error")
	}
	if len(cleaner.calls) != 0 {
		t.Fatalf("cleaner must not run when the row delete failed, ran %d times", len(cleaner.calls))
	}
}

func TestService_Delete_NilCleanerIsNoop(t *testing.T) {
	repo := &mockRepo{deleteFn: func(_ context.Context, _, _ string) error { return nil }}
	svc := NewService(repo, nil, nil) // no cleaner wired

	if err := svc.Delete(context.Background(), "u1", "t1"); err != nil {
		t.Fatalf("unexpected err with nil cleaner: %v", err)
	}
}

// ── recurrence successor trigger (E-050 / NIC-1773) ──────────────────────────

// fakeMaterializer records the successor calls the task service makes.
type fakeMaterializer struct {
	calls []string
	err   error
}

func (f *fakeMaterializer) MaterializeAfterCompletion(_ context.Context, _, ruleID string) error {
	f.calls = append(f.calls, ruleID)
	return f.err
}

// setStatusSvc wires a service whose update() returns the given task.
func setStatusSvc(t *testing.T, stored Task, m RecurrenceMaterializer) Service {
	t.Helper()
	repo := &mockRepo{
		getByID: func(context.Context, string, string) (*Task, error) { return &stored, nil },
		update: func(_ context.Context, _, _ string, req UpdateTaskRequest, _ completedAtChange) (Task, error) {
			out := stored
			if req.Status != nil {
				out.Status = *req.Status
			}
			return out, nil
		},
		countActive: func(context.Context, string, string) (int, error) { return 0, nil },
	}
	return NewService(repo, nil, nil).WithMaterializer(m)
}

func recurringStored() Task {
	ruleID := "rule-1"
	occ := "2026-03-02"
	return Task{
		ID: "t1", UserID: "u1", ProjectID: "p1", Title: "Water", Status: "active",
		Priority: "medium", Energy: "medium",
		RecurrenceRuleID: &ruleID, OccurrenceDate: &occ,
	}
}

// Completing a recurring occurrence materializes its successor in the same request.
func TestSetStatus_CompletingRecurringMaterializesSuccessor(t *testing.T) {
	m := &fakeMaterializer{}
	svc := setStatusSvc(t, recurringStored(), m)

	if _, err := svc.SetStatus(context.Background(), "u1", "t1", "free", statusDone); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if len(m.calls) != 1 || m.calls[0] != "rule-1" {
		t.Errorf("materializer calls = %v, want [rule-1]", m.calls)
	}
}

// The edit dialog completes a task through the plain PATCH, not the status
// endpoint — it must spawn the successor too, or the series silently ends.
func TestUpdate_CompletingRecurringMaterializesSuccessor(t *testing.T) {
	m := &fakeMaterializer{}
	svc := setStatusSvc(t, recurringStored(), m)

	done := statusDone
	if _, err := svc.Update(context.Background(), "u1", "t1", "free", UpdateTaskRequest{Status: &done}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(m.calls) != 1 || m.calls[0] != "rule-1" {
		t.Errorf("materializer calls = %v, want [rule-1]", m.calls)
	}
}

// Completing through the status endpoint must not double-fire now that the
// trigger lives in the shared update path.
func TestSetStatus_MaterializesSuccessorExactlyOnce(t *testing.T) {
	m := &fakeMaterializer{}
	svc := setStatusSvc(t, recurringStored(), m)

	if _, err := svc.SetStatus(context.Background(), "u1", "t1", "free", statusDone); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if len(m.calls) != 1 {
		t.Errorf("materializer calls = %v, want exactly one", m.calls)
	}
}

// Only completion triggers a successor, and only for recurring tasks.
func TestSetStatus_SuccessorNotTriggered(t *testing.T) {
	tests := []struct {
		name   string
		stored Task
		status string
	}{
		{"recurring but cancelled", recurringStored(), "cancelled"},
		{"recurring but merely active", recurringStored(), "active"},
		{"non-recurring completed", Task{ID: "t1", UserID: "u1", ProjectID: "p1", Status: "active"}, statusDone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &fakeMaterializer{}
			svc := setStatusSvc(t, tt.stored, m)

			if _, err := svc.SetStatus(context.Background(), "u1", "t1", "free", tt.status); err != nil {
				t.Fatalf("SetStatus: %v", err)
			}
			if len(m.calls) != 0 {
				t.Errorf("materializer calls = %v, want none", m.calls)
			}
		})
	}
}

// The successor is best-effort: a failure is swallowed so the status change the
// user already made still succeeds. The cron sweep retries.
func TestSetStatus_SuccessorFailureDoesNotFailTheRequest(t *testing.T) {
	m := &fakeMaterializer{err: errors.New("db down")}
	svc := setStatusSvc(t, recurringStored(), m)

	view, err := svc.SetStatus(context.Background(), "u1", "t1", "free", statusDone)
	if err != nil {
		t.Fatalf("SetStatus = %v, want the status change to survive a failed successor", err)
	}
	if view.Status != statusDone {
		t.Errorf("status = %q, want %q", view.Status, statusDone)
	}
}

// A nil materializer (feature unwired) must not panic.
func TestSetStatus_NilMaterializerIsSafe(t *testing.T) {
	svc := setStatusSvc(t, recurringStored(), nil)
	if _, err := svc.SetStatus(context.Background(), "u1", "t1", "free", statusDone); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
}

// ── Skip (NIC-1997) ──────────────────────────────────────────────────────────

type fakeCanceller struct {
	calls []struct{ userID, taskID string }
	err   error
}

func (f *fakeCanceller) CancelForTask(_ context.Context, userID, taskID string) error {
	f.calls = append(f.calls, struct{ userID, taskID string }{userID, taskID})
	return f.err
}

func TestService_Skip_HappyPath(t *testing.T) {
	stored := &Task{ID: "t1", UserID: "u1", ProjectID: "p1", Status: statusActive, RecurrenceRuleID: ptr("r1")}
	skipped := *stored
	skipped.OccurrenceStatus = ptr("skipped")

	repo := &mockRepo{
		getByID:     func(_ context.Context, _, _ string) (*Task, error) { return stored, nil },
		markSkipped: func(_ context.Context, _, _ string) (*Task, error) { return &skipped, nil },
	}
	canceller := &fakeCanceller{}
	svc := NewService(repo, nil, nil).WithNotificationCanceller(canceller)

	view, err := svc.Skip(context.Background(), "u1", "t1")
	if err != nil {
		t.Fatalf("Skip: %v", err)
	}
	if view.OccurrenceStatus == nil || *view.OccurrenceStatus != "skipped" {
		t.Errorf("occurrenceStatus = %v, want skipped", view.OccurrenceStatus)
	}
	if len(canceller.calls) != 1 || canceller.calls[0].taskID != "t1" {
		t.Errorf("canceller calls = %+v, want one call for t1", canceller.calls)
	}
}

func TestService_Skip_ConflictBranches(t *testing.T) {
	tests := []struct {
		name     string
		stored   *Task
		wantCode string
	}{
		{"not recurring", &Task{ID: "t1", UserID: "u1", Status: statusActive}, apperror.ErrTaskNotRecurring},
		{"already skipped", &Task{ID: "t1", UserID: "u1", Status: statusActive, RecurrenceRuleID: ptr("r1"), OccurrenceStatus: ptr("skipped")}, apperror.ErrTaskAlreadySkipped},
		{"already missed", &Task{ID: "t1", UserID: "u1", Status: statusActive, RecurrenceRuleID: ptr("r1"), OccurrenceStatus: ptr("missed")}, apperror.ErrTaskAlreadyMissed},
		{"not active", &Task{ID: "t1", UserID: "u1", Status: statusDone, RecurrenceRuleID: ptr("r1")}, apperror.ErrTaskNotActive},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockRepo{
				getByID: func(_ context.Context, _, _ string) (*Task, error) { return tt.stored, nil },
				markSkipped: func(_ context.Context, _, _ string) (*Task, error) {
					t.Fatal("MarkSkipped must not be called when the eligibility guard rejects the task")
					return nil, nil
				},
			}
			svc := NewService(repo, nil, nil)

			_, err := svc.Skip(context.Background(), "u1", "t1")
			ae := appErr(err)
			if ae == nil || ae.Code != tt.wantCode {
				t.Fatalf("err = %v, want %s", err, tt.wantCode)
			}
		})
	}
}

func TestService_Skip_NotFound(t *testing.T) {
	repo := &mockRepo{
		getByID: func(_ context.Context, _, _ string) (*Task, error) {
			return nil, apperror.New(http.StatusNotFound, apperror.ErrTaskNotFound, "task not found")
		},
	}
	svc := NewService(repo, nil, nil)

	_, err := svc.Skip(context.Background(), "u1", "missing")
	ae := appErr(err)
	if ae == nil || ae.Code != apperror.ErrTaskNotFound {
		t.Fatalf("err = %v, want TASK_NOT_FOUND", err)
	}
}

// A race between the eligibility GetByID and the write (e.g. concurrently
// reaped) surfaces the precise reason from a fresh read, not a bare 409.
func TestService_Skip_RaceLostReDerivesReason(t *testing.T) {
	stored := &Task{ID: "t1", UserID: "u1", Status: statusActive, RecurrenceRuleID: ptr("r1")}
	refetched := &Task{ID: "t1", UserID: "u1", Status: statusActive, RecurrenceRuleID: ptr("r1"), OccurrenceStatus: ptr("missed")}
	calls := 0
	repo := &mockRepo{
		getByID: func(_ context.Context, _, _ string) (*Task, error) {
			calls++
			if calls == 1 {
				return stored, nil
			}
			return refetched, nil
		},
		markSkipped: func(_ context.Context, _, _ string) (*Task, error) { return nil, nil },
	}
	svc := NewService(repo, nil, nil)

	_, err := svc.Skip(context.Background(), "u1", "t1")
	ae := appErr(err)
	if ae == nil || ae.Code != apperror.ErrTaskAlreadyMissed {
		t.Fatalf("err = %v, want TASK_ALREADY_MISSED", err)
	}
	if calls != 2 {
		t.Errorf("GetByID calls = %d, want 2 (guard + re-derive)", calls)
	}
}

func TestService_Skip_NilCancellerIsSafe(t *testing.T) {
	stored := &Task{ID: "t1", UserID: "u1", Status: statusActive, RecurrenceRuleID: ptr("r1")}
	skipped := *stored
	skipped.OccurrenceStatus = ptr("skipped")
	repo := &mockRepo{
		getByID:     func(_ context.Context, _, _ string) (*Task, error) { return stored, nil },
		markSkipped: func(_ context.Context, _, _ string) (*Task, error) { return &skipped, nil },
	}
	svc := NewService(repo, nil, nil) // no WithNotificationCanceller

	if _, err := svc.Skip(context.Background(), "u1", "t1"); err != nil {
		t.Fatalf("Skip: %v", err)
	}
}

// ── Delete: recurring live-instance guard (NIC-1997) ────────────────────────

func TestService_Delete_RejectsLiveRecurringInstance(t *testing.T) {
	stored := &Task{ID: "t1", UserID: "u1", Status: statusActive, RecurrenceRuleID: ptr("r1")}
	repo := &mockRepo{
		getByID: func(_ context.Context, _, _ string) (*Task, error) { return stored, nil },
		deleteFn: func(_ context.Context, _, _ string) error {
			t.Fatal("Delete must not reach the repo when the live-instance guard rejects the task")
			return nil
		},
	}
	svc := NewService(repo, nil, nil)

	err := svc.Delete(context.Background(), "u1", "t1")
	ae := appErr(err)
	if ae == nil || ae.Code != apperror.ErrRecurringLiveInstance {
		t.Fatalf("err = %v, want RECURRING_LIVE_INSTANCE", err)
	}
}

func TestService_Delete_AllowsHistoricalRecurringRows(t *testing.T) {
	tests := []struct {
		name   string
		stored *Task
	}{
		{"done", &Task{ID: "t1", UserID: "u1", Status: statusDone, RecurrenceRuleID: ptr("r1")}},
		{"cancelled", &Task{ID: "t1", UserID: "u1", Status: "cancelled", RecurrenceRuleID: ptr("r1")}},
		{"skipped occurrence", &Task{ID: "t1", UserID: "u1", Status: statusActive, RecurrenceRuleID: ptr("r1"), OccurrenceStatus: ptr("skipped")}},
		{"missed occurrence", &Task{ID: "t1", UserID: "u1", Status: statusActive, RecurrenceRuleID: ptr("r1"), OccurrenceStatus: ptr("missed")}},
		{"non-recurring", &Task{ID: "t1", UserID: "u1", Status: statusActive}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deleted := false
			repo := &mockRepo{
				getByID:  func(_ context.Context, _, _ string) (*Task, error) { return tt.stored, nil },
				deleteFn: func(_ context.Context, _, _ string) error { deleted = true; return nil },
			}
			svc := NewService(repo, nil, nil)

			if err := svc.Delete(context.Background(), "u1", "t1"); err != nil {
				t.Fatalf("Delete: %v", err)
			}
			if !deleted {
				t.Error("expected repo.Delete to be called for a historical/non-recurring row")
			}
		})
	}
}

// requireSkippable exposes paused/cancelled codes not in the shared ConflictBranches table.
func TestRequireSkippable_PausedAndCancelled(t *testing.T) {
	tests := []struct {
		name     string
		task     *Task
		wantCode string
	}{
		{
			"paused occurrence",
			&Task{ID: "t1", UserID: "u1", Status: statusActive, RecurrenceRuleID: ptr("r1"), OccurrenceStatus: ptr("paused")},
			apperror.ErrTaskAlreadyPaused,
		},
		{
			"cancelled occurrence",
			&Task{ID: "t1", UserID: "u1", Status: "cancelled", RecurrenceRuleID: ptr("r1"), OccurrenceStatus: ptr("cancelled")},
			apperror.ErrTaskAlreadyCancelled,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := requireSkippable(tt.task)
			ae := appErr(err)
			if ae == nil || ae.Code != tt.wantCode {
				t.Fatalf("requireSkippable = %v, want %s", err, tt.wantCode)
			}
		})
	}
}

// MarkMissed must call materializeSuccessor on a successful miss so the series
// continues without waiting for the hourly sweep.
func TestMarkMissed_MaterializesSuccessor(t *testing.T) {
	ruleID := "rule-1"
	missed := &Task{
		ID: "t1", UserID: "u1", ProjectID: "p1", Title: "Daily walk", Status: "cancelled",
		RecurrenceRuleID: &ruleID, OccurrenceStatus: ptr("missed"),
	}
	repo := &mockRepo{
		markMissed: func(_ context.Context, _, _ string) (*Task, error) { return missed, nil },
	}
	m := &fakeMaterializer{}
	svc := NewService(repo, nil, nil).WithMaterializer(m)

	if _, err := svc.MarkMissed(context.Background(), "u1", "t1"); err != nil {
		t.Fatalf("MarkMissed: %v", err)
	}
	if len(m.calls) != 1 || m.calls[0] != ruleID {
		t.Errorf("materializer calls = %v, want [%s]", m.calls, ruleID)
	}
}

// MarkMissed with no successor materializer wired must not panic.
func TestMarkMissed_NilMaterializerIsSafe(t *testing.T) {
	ruleID := "rule-1"
	missed := &Task{
		ID: "t1", UserID: "u1", ProjectID: "p1", Status: "cancelled",
		RecurrenceRuleID: &ruleID, OccurrenceStatus: ptr("missed"),
	}
	repo := &mockRepo{
		markMissed: func(_ context.Context, _, _ string) (*Task, error) { return missed, nil },
	}
	svc := NewService(repo, nil, nil)

	if _, err := svc.MarkMissed(context.Background(), "u1", "t1"); err != nil {
		t.Fatalf("MarkMissed: %v", err)
	}
}

// Schedule must reject a live recurring instance with TASK_RECURRING_NOT_RESCHEDULABLE.
func TestSchedule_RejectsLiveRecurringInstance(t *testing.T) {
	ruleID := "rule-1"
	live := &Task{
		ID: "t1", UserID: "u1", Status: statusActive,
		RecurrenceRuleID: &ruleID,
	}
	repo := &mockRepo{
		getByID: func(_ context.Context, _, _ string) (*Task, error) { return live, nil },
	}
	svc := NewService(repo, nil, nil)

	_, err := svc.Schedule(context.Background(), "u1", "t1", "free", ScheduleRequest{ScheduledFor: ptr("2026-10-01")})
	ae := appErr(err)
	if ae == nil || ae.Code != apperror.ErrTaskRecurringNotReschedulable {
		t.Fatalf("err = %v, want TASK_RECURRING_NOT_RESCHEDULABLE", err)
	}
}

// Schedule must allow non-recurring tasks and historical recurring rows.
func TestSchedule_AllowsNonLiveRows(t *testing.T) {
	ruleID := "rule-1"
	tests := []struct {
		name string
		task *Task
	}{
		{"non-recurring", &Task{ID: "t1", UserID: "u1", Status: statusActive}},
		{"done recurring", &Task{ID: "t1", UserID: "u1", Status: statusDone, RecurrenceRuleID: &ruleID}},
		{"skipped occurrence", &Task{ID: "t1", UserID: "u1", Status: statusActive, RecurrenceRuleID: &ruleID, OccurrenceStatus: ptr("skipped")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockRepo{
				getByID: func(_ context.Context, _, _ string) (*Task, error) { return tt.task, nil },
				updateSchedule: func(_ context.Context, _, _ string, sf, st *string, _ *bool) (Task, error) {
					return Task{ID: "t1", ScheduledFor: sf, ScheduledTime: st}, nil
				},
			}
			if _, err := NewService(repo, nil, nil).Schedule(context.Background(), "u1", "t1", "free", ScheduleRequest{ScheduledFor: ptr("2026-10-01")}); err != nil {
				t.Fatalf("%s: unexpected error: %v", tt.name, err)
			}
		})
	}
}

// update() must block done→active on a recurring occurrence.
func TestUpdate_RejectsReopenOfRecurringOccurrence(t *testing.T) {
	ruleID := "rule-1"
	stored := Task{
		ID: "t1", UserID: "u1", ProjectID: "p1", Status: statusDone,
		RecurrenceRuleID: &ruleID,
	}
	repo := &mockRepo{
		getByID:     func(_ context.Context, _, _ string) (*Task, error) { return &stored, nil },
		countActive: func(_ context.Context, _, _ string) (int, error) { return 0, nil },
	}
	svc := NewService(repo, nil, nil)

	_, err := svc.Update(context.Background(), "u1", "t1", "free", UpdateTaskRequest{Status: ptr(statusActive)})
	ae := appErr(err)
	if ae == nil || ae.Code != apperror.ErrTaskRecurringNotReversible {
		t.Fatalf("err = %v, want TASK_RECURRING_NOT_REVERSIBLE", err)
	}
}

// update() must allow done→active on a non-recurring task.
func TestUpdate_AllowsReopenOfNonRecurringTask(t *testing.T) {
	stored := Task{ID: "t1", UserID: "u1", ProjectID: "p1", Status: statusDone}
	repo := &mockRepo{
		getByID: func(_ context.Context, _, _ string) (*Task, error) { return &stored, nil },
		update: func(_ context.Context, _, _ string, req UpdateTaskRequest, _ completedAtChange) (Task, error) {
			out := stored
			if req.Status != nil {
				out.Status = *req.Status
			}
			return out, nil
		},
		countActive: func(_ context.Context, _, _ string) (int, error) { return 0, nil },
	}
	svc := NewService(repo, nil, nil)

	view, err := svc.Update(context.Background(), "u1", "t1", "free", UpdateTaskRequest{Status: ptr(statusActive)})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if view.Status != statusActive {
		t.Errorf("status = %q, want active", view.Status)
	}
}
