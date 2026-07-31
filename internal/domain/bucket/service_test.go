package bucket_test

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/bucket"
	"github.com/nicoflow/nicoflow-api/internal/domain/task"
)

// ── mocks ─────────────────────────────────────────────────────────────────────

type mockRepo struct {
	create           func(ctx context.Context, b bucket.Bucket) (bucket.Bucket, error)
	listByUser       func(ctx context.Context, userID string) ([]bucket.Bucket, error)
	getByID          func(ctx context.Context, userID, id string) (bucket.Bucket, error)
	updateContent    func(ctx context.Context, userID, id, content string) (bucket.Bucket, error)
	delete           func(ctx context.Context, userID, id string) error
	countUnprocessed func(ctx context.Context, userID string) (int, error)
	markProcessed    func(ctx context.Context, userID, id, result string, taskID, projectID *string) (bucket.Bucket, error)
}

func (m *mockRepo) Create(ctx context.Context, b bucket.Bucket) (bucket.Bucket, error) {
	return m.create(ctx, b)
}
func (m *mockRepo) ListByUser(ctx context.Context, userID string) ([]bucket.Bucket, error) {
	return m.listByUser(ctx, userID)
}
func (m *mockRepo) GetByID(ctx context.Context, userID, id string) (bucket.Bucket, error) {
	return m.getByID(ctx, userID, id)
}
func (m *mockRepo) UpdateContent(ctx context.Context, userID, id, content string) (bucket.Bucket, error) {
	return m.updateContent(ctx, userID, id, content)
}
func (m *mockRepo) Delete(ctx context.Context, userID, id string) error {
	return m.delete(ctx, userID, id)
}
func (m *mockRepo) CountUnprocessed(ctx context.Context, userID string) (int, error) {
	if m.countUnprocessed == nil {
		return 0, nil
	}
	return m.countUnprocessed(ctx, userID)
}
func (m *mockRepo) MarkProcessed(ctx context.Context, userID, id, result string, taskID, projectID *string) (bucket.Bucket, error) {
	return m.markProcessed(ctx, userID, id, result, taskID, projectID)
}

type mockTaskCreator struct {
	create func(ctx context.Context, userID, projectID, plan string, req task.CreateTaskRequest) (task.TaskView, error)
}

func (m *mockTaskCreator) CreateWithoutEvent(ctx context.Context, userID, projectID, plan string, req task.CreateTaskRequest) (task.TaskView, error) {
	return m.create(ctx, userID, projectID, plan, req)
}

// ── helpers ───────────────────────────────────────────────────────────────────

func appErr(err error) *apperror.AppError {
	var ae *apperror.AppError
	if errors.As(err, &ae) {
		return ae
	}
	return nil
}

func ptr[T any](v T) *T { return &v }

func unprocessed(id string) bucket.Bucket {
	return bucket.Bucket{ID: id, UserID: "u1", Content: "note", CreatedAt: time.Now(), UpdatedAt: time.Now()}
}

// ── Create ────────────────────────────────────────────────────────────────────

func TestService_Create(t *testing.T) {
	tests := []struct {
		name       string
		content    string
		wantCode   string
		wantStatus int
	}{
		{name: "happy path", content: "buy milk"},
		{name: "trims and stores", content: "  hello  "},
		{name: "empty", content: "", wantCode: apperror.ErrInvalidInput, wantStatus: http.StatusUnprocessableEntity},
		{name: "whitespace only", content: "   ", wantCode: apperror.ErrInvalidInput, wantStatus: http.StatusUnprocessableEntity},
		{name: "at 500 boundary is allowed", content: strings.Repeat("a", 500)},
		{name: "over 500 rejected", content: strings.Repeat("a", 501), wantCode: apperror.ErrInvalidInput, wantStatus: http.StatusUnprocessableEntity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockRepo{
				create: func(_ context.Context, b bucket.Bucket) (bucket.Bucket, error) {
					b.CreatedAt, b.UpdatedAt = time.Now(), time.Now()
					return b, nil
				},
			}
			svc := bucket.NewService(repo, &mockTaskCreator{}, nil, nil)
			view, err := svc.Create(context.Background(), "u1", tt.content)

			if tt.wantCode != "" {
				ae := appErr(err)
				if ae == nil || ae.Code != tt.wantCode || ae.Status != tt.wantStatus {
					t.Fatalf("want %s/%d, got %v", tt.wantCode, tt.wantStatus, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if view.Content != strings.TrimSpace(tt.content) {
				t.Errorf("content = %q, want trimmed %q", view.Content, strings.TrimSpace(tt.content))
			}
			if view.CreatedNoteID != nil {
				t.Errorf("createdNoteId should always be null, got %v", *view.CreatedNoteID)
			}
		})
	}
}

// ── Update ────────────────────────────────────────────────────────────────────

func TestService_Update(t *testing.T) {
	notFound := apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "bucket item not found")
	conflict := apperror.New(http.StatusConflict, apperror.ErrConflict, "bucket item is already processed")

	tests := []struct {
		name       string
		content    string
		repoErr    error
		wantCode   string
		wantStatus int
	}{
		{name: "happy path", content: "edited"},
		{name: "empty content", content: "  ", wantCode: apperror.ErrInvalidInput, wantStatus: http.StatusUnprocessableEntity},
		{name: "not found", content: "x", repoErr: notFound, wantCode: apperror.ErrResourceNotFound, wantStatus: http.StatusNotFound},
		{name: "already processed", content: "x", repoErr: conflict, wantCode: apperror.ErrConflict, wantStatus: http.StatusConflict},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockRepo{
				updateContent: func(_ context.Context, _, id, content string) (bucket.Bucket, error) {
					if tt.repoErr != nil {
						return bucket.Bucket{}, tt.repoErr
					}
					b := unprocessed(id)
					b.Content = content
					return b, nil
				},
			}
			svc := bucket.NewService(repo, &mockTaskCreator{}, nil, nil)
			_, err := svc.Update(context.Background(), "u1", "b1", tt.content)

			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			ae := appErr(err)
			if ae == nil || ae.Code != tt.wantCode || ae.Status != tt.wantStatus {
				t.Fatalf("want %s/%d, got %v", tt.wantCode, tt.wantStatus, err)
			}
		})
	}
}

// ── Process ───────────────────────────────────────────────────────────────────

func TestService_Process_Trash(t *testing.T) {
	var gotResult string
	repo := &mockRepo{
		markProcessed: func(_ context.Context, _, id, result string, taskID, projectID *string) (bucket.Bucket, error) {
			gotResult = result
			if taskID != nil || projectID != nil {
				t.Errorf("trash must not set taskID/projectID, got %v/%v", taskID, projectID)
			}
			b := unprocessed(id)
			b.ProcessingResult = &result
			return b, nil
		},
	}
	svc := bucket.NewService(repo, &mockTaskCreator{}, nil, nil)
	_, err := svc.Process(context.Background(), "u1", "b1", "free", bucket.ProcessBucketRequest{ProcessingResult: bucket.ResultTrash})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotResult != bucket.ResultTrash {
		t.Errorf("result = %q, want trash", gotResult)
	}
}

func TestService_Process_Note_NotImplemented(t *testing.T) {
	svc := bucket.NewService(&mockRepo{}, &mockTaskCreator{}, nil, nil)
	_, err := svc.Process(context.Background(), "u1", "b1", "free", bucket.ProcessBucketRequest{ProcessingResult: bucket.ResultNote})
	ae := appErr(err)
	if ae == nil || ae.Status != http.StatusNotImplemented {
		t.Fatalf("want 501, got %v", err)
	}
}

func TestService_Process_InvalidResult(t *testing.T) {
	svc := bucket.NewService(&mockRepo{}, &mockTaskCreator{}, nil, nil)
	_, err := svc.Process(context.Background(), "u1", "b1", "free", bucket.ProcessBucketRequest{ProcessingResult: "bogus"})
	ae := appErr(err)
	if ae == nil || ae.Code != apperror.ErrInvalidInput || ae.Status != http.StatusUnprocessableEntity {
		t.Fatalf("want INVALID_INPUT/422, got %v", err)
	}
}

func TestService_Process_Task_RequiresProjectAndDetails(t *testing.T) {
	details := &bucket.ProcessTaskDetails{Title: "t"}
	tests := []struct {
		name string
		req  bucket.ProcessBucketRequest
	}{
		{name: "missing projectId", req: bucket.ProcessBucketRequest{ProcessingResult: bucket.ResultTask, TaskDetails: details}},
		{name: "empty projectId", req: bucket.ProcessBucketRequest{ProcessingResult: bucket.ResultTask, ProjectID: ptr(""), TaskDetails: details}},
		{name: "missing taskDetails", req: bucket.ProcessBucketRequest{ProcessingResult: bucket.ResultTask, ProjectID: ptr("p1")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := bucket.NewService(&mockRepo{}, &mockTaskCreator{}, nil, nil)
			_, err := svc.Process(context.Background(), "u1", "b1", "free", tt.req)
			ae := appErr(err)
			if ae == nil || ae.Code != apperror.ErrInvalidInput || ae.Status != http.StatusUnprocessableEntity {
				t.Fatalf("want INVALID_INPUT/422, got %v", err)
			}
		})
	}
}

func TestService_Process_Task_AlreadyProcessed(t *testing.T) {
	taskCalled := false
	repo := &mockRepo{
		getByID: func(_ context.Context, _, id string) (bucket.Bucket, error) {
			b := unprocessed(id)
			now := time.Now()
			b.ProcessedAt = &now // already processed
			return b, nil
		},
	}
	tc := &mockTaskCreator{create: func(context.Context, string, string, string, task.CreateTaskRequest) (task.TaskView, error) {
		taskCalled = true
		return task.TaskView{}, nil
	}}
	svc := bucket.NewService(repo, tc, nil, nil)
	_, err := svc.Process(context.Background(), "u1", "b1", "free", bucket.ProcessBucketRequest{
		ProcessingResult: bucket.ResultTask, ProjectID: ptr("p1"), TaskDetails: &bucket.ProcessTaskDetails{Title: "t"},
	})
	ae := appErr(err)
	if ae == nil || ae.Code != apperror.ErrConflict || ae.Status != http.StatusConflict {
		t.Fatalf("want CONFLICT/409, got %v", err)
	}
	if taskCalled {
		t.Error("task must NOT be created when the item is already processed")
	}
}

// The core NFR: a plan-limit failure from task.Create must abort BEFORE the
// bucket is marked, so no task and no processed-mark happen.
func TestService_Process_Task_PlanLimitAbortsBeforeMark(t *testing.T) {
	marked := false
	repo := &mockRepo{
		getByID: func(_ context.Context, _, id string) (bucket.Bucket, error) { return unprocessed(id), nil },
		markProcessed: func(context.Context, string, string, string, *string, *string) (bucket.Bucket, error) {
			marked = true
			return bucket.Bucket{}, nil
		},
	}
	planErr := apperror.New(http.StatusForbidden, apperror.ErrPlanLimitExceeded, "limit")
	tc := &mockTaskCreator{create: func(context.Context, string, string, string, task.CreateTaskRequest) (task.TaskView, error) {
		return task.TaskView{}, planErr
	}}
	svc := bucket.NewService(repo, tc, nil, nil)
	_, err := svc.Process(context.Background(), "u1", "b1", "free", bucket.ProcessBucketRequest{
		ProcessingResult: bucket.ResultTask, ProjectID: ptr("p1"), TaskDetails: &bucket.ProcessTaskDetails{Title: "t"},
	})
	ae := appErr(err)
	if ae == nil || ae.Code != apperror.ErrPlanLimitExceeded {
		t.Fatalf("want PLAN_LIMIT_EXCEEDED, got %v", err)
	}
	if marked {
		t.Error("bucket must stay unprocessed when task creation fails")
	}
}

func TestService_Process_Task_HappyPath_MapsDetailsAndMarks(t *testing.T) {
	var gotReq task.CreateTaskRequest
	var gotResult string
	var gotTaskID, gotProjectID *string
	repo := &mockRepo{
		getByID: func(_ context.Context, _, id string) (bucket.Bucket, error) { return unprocessed(id), nil },
		markProcessed: func(_ context.Context, _, id, result string, taskID, projectID *string) (bucket.Bucket, error) {
			gotResult, gotTaskID, gotProjectID = result, taskID, projectID
			b := unprocessed(id)
			b.ProcessingResult, b.CreatedTaskID, b.ProjectID = &result, taskID, projectID
			return b, nil
		},
	}
	tc := &mockTaskCreator{create: func(_ context.Context, _, projectID, _ string, req task.CreateTaskRequest) (task.TaskView, error) {
		gotReq = req
		return task.TaskView{ID: "task-123", ProjectID: projectID}, nil
	}}
	svc := bucket.NewService(repo, tc, nil, nil)
	view, err := svc.Process(context.Background(), "u1", "b1", "free", bucket.ProcessBucketRequest{
		ProcessingResult: bucket.ResultTask,
		ProjectID:        ptr("p1"),
		TaskDetails: &bucket.ProcessTaskDetails{
			Title: "Write report", Priority: ptr("high"), Energy: ptr("deep"),
			RollsOver: ptr(false), ScheduledFor: ptr("2026-08-01"), EstimatedMinutes: ptr(30),
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertMappedTaskRequest(t, gotReq)
	assertMarkArgs(t, gotResult, gotTaskID, gotProjectID)
	if view.CreatedTaskID == nil || *view.CreatedTaskID != "task-123" {
		t.Errorf("view.createdTaskId = %v, want task-123", view.CreatedTaskID)
	}
}

// A field the dialog leaves blank must arrive unset so the task service applies
// its own default rather than the process path inventing one.
func TestService_Process_Task_OmittedFieldsFallThroughToDefaults(t *testing.T) {
	var gotReq task.CreateTaskRequest
	repo := &mockRepo{
		getByID: func(_ context.Context, _, id string) (bucket.Bucket, error) { return unprocessed(id), nil },
		markProcessed: func(_ context.Context, _, id, _ string, _, _ *string) (bucket.Bucket, error) {
			return unprocessed(id), nil
		},
	}
	tc := &mockTaskCreator{create: func(_ context.Context, _, _, _ string, req task.CreateTaskRequest) (task.TaskView, error) {
		gotReq = req
		return task.TaskView{ID: "task-123"}, nil
	}}
	svc := bucket.NewService(repo, tc, nil, nil)
	_, err := svc.Process(context.Background(), "u1", "b1", "free", bucket.ProcessBucketRequest{
		ProcessingResult: bucket.ResultTask,
		ProjectID:        ptr("p1"),
		TaskDetails:      &bucket.ProcessTaskDetails{Title: "Bare"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotReq.Priority != "" || gotReq.Energy != "" || gotReq.Status != "" ||
		gotReq.RollsOver != nil || gotReq.ScheduledFor != nil {
		t.Errorf("omitted fields must stay unset: %+v", gotReq)
	}
}

// assertMappedTaskRequest checks every field the process dialog offers reaches
// the task create contract. Status alone stays unset — the task service owns it.
func assertMappedTaskRequest(t *testing.T, req task.CreateTaskRequest) {
	t.Helper()
	if req.Title != "Write report" || req.Priority != "high" || req.Energy != "deep" {
		t.Errorf("mapping wrong: %+v", req)
	}
	if req.EstimatedMinutes == nil || *req.EstimatedMinutes != 30 {
		t.Errorf("estimatedMinutes not forwarded: %+v", req)
	}
	if req.RollsOver == nil || *req.RollsOver {
		t.Errorf("rollsOver not forwarded: %+v", req)
	}
	if req.ScheduledFor == nil || *req.ScheduledFor != "2026-08-01" {
		t.Errorf("scheduledFor not forwarded: %+v", req)
	}
	if req.Status != "" {
		t.Errorf("status must be unset: %+v", req)
	}
}

func assertMarkArgs(t *testing.T, result string, taskID, projectID *string) {
	t.Helper()
	if result != bucket.ResultTask || taskID == nil || *taskID != "task-123" || projectID == nil || *projectID != "p1" {
		t.Errorf("mark args wrong: result=%s taskID=%v projectID=%v", result, taskID, projectID)
	}
}
