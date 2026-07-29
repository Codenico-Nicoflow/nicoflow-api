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
	listByProject    func(ctx context.Context, userID, projectID string, f ListTasksFilter) ([]Task, error)
	getByID          func(ctx context.Context, userID, id string) (*Task, error)
	create           func(ctx context.Context, t Task) (Task, error)
	update           func(ctx context.Context, userID, id string, req UpdateTaskRequest, ca completedAtChange) (Task, error)
	deleteFn         func(ctx context.Context, userID, id string) error
	projectOwned     func(ctx context.Context, userID, projectID string) (bool, error)
	countActiveInbox func(ctx context.Context, userID, projectID string) (int, error)
	countNonTerminal func(ctx context.Context, userID, projectID string) (int, error)
	nextDisplayOrder func(ctx context.Context, userID, projectID string) (int, error)
	updateSchedule   func(ctx context.Context, userID, id string, scheduledFor *string, rollsOver *bool) (Task, error)
	repack           func(ctx context.Context, userID, id string, targetOrder int) (Task, error)
	listActiveInbox  func(ctx context.Context, userID string) ([]Task, error)
	existsByID       func(ctx context.Context, id string) (bool, error)
	isOpenable       func(ctx context.Context, userID, id string) (bool, error)
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
func (m *mockRepo) CountNonTerminalByProject(ctx context.Context, userID, projectID string) (int, error) {
	if m.countNonTerminal == nil {
		return 0, nil
	}
	return m.countNonTerminal(ctx, userID, projectID)
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
			wantStatus: "inbox",
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
	svc := NewService(repo, nil, nil)

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
		countActiveInbox: func(_ context.Context, _, _ string) (int, error) { return 0, nil },
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
				updateSchedule: func(_ context.Context, _, _ string, sf *string, _ *bool) (Task, error) {
					return Task{ID: "t1", ScheduledFor: sf}, nil
				},
			}
			_, err := NewService(repo, nil, nil).Schedule(context.Background(), "u1", "t1", ScheduleRequest{ScheduledFor: tt.date})
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
		countActiveInbox: func(context.Context, string, string) (int, error) { return 0, nil },
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

// Only completion triggers a successor, and only for recurring tasks.
func TestSetStatus_SuccessorNotTriggered(t *testing.T) {
	tests := []struct {
		name   string
		stored Task
		status string
	}{
		{"recurring but cancelled", recurringStored(), "cancelled"},
		{"recurring but merely active", recurringStored(), "active"},
		{"recurring but missed", recurringStored(), statusMissed},
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

// `missed` is a valid status on the enum but must never count against the plan
// limit — an ignored daily rule would otherwise fill the 50-task cap invisibly.
func TestMissedStatus_AllowedButNotPlanCounted(t *testing.T) {
	if !allowedStatuses[statusMissed] {
		t.Error("missed must be an accepted status")
	}
	if activeInboxStatuses[statusMissed] {
		t.Error("missed must not count against the plan limit")
	}
}
