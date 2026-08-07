package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// stubTasks lets each test wire in the one method it cares about.
type stubTasks struct {
	get        func(ctx context.Context, userID, id string) (TaskViewJSON, error)
	create     func(ctx context.Context, userID, projectID, plan string, req CreateTaskInput) (TaskViewJSON, error)
	setStatus  func(ctx context.Context, userID, id, plan, status string) (TaskViewJSON, error)
	schedule   func(ctx context.Context, userID, id, plan string, req ScheduleInput) (TaskViewJSON, error)
	listForUsr func(ctx context.Context, userID string, f UserListInput) (TaskListJSON, error)
}

func (s stubTasks) Get(ctx context.Context, userID, id string) (TaskViewJSON, error) {
	return s.get(ctx, userID, id)
}
func (s stubTasks) Create(ctx context.Context, userID, projectID, plan string, req CreateTaskInput) (TaskViewJSON, error) {
	return s.create(ctx, userID, projectID, plan, req)
}
func (s stubTasks) SetStatus(ctx context.Context, userID, id, plan, status string) (TaskViewJSON, error) {
	return s.setStatus(ctx, userID, id, plan, status)
}
func (s stubTasks) Schedule(ctx context.Context, userID, id, plan string, req ScheduleInput) (TaskViewJSON, error) {
	return s.schedule(ctx, userID, id, plan, req)
}
func (s stubTasks) ListForUser(ctx context.Context, userID string, f UserListInput) (TaskListJSON, error) {
	return s.listForUsr(ctx, userID, f)
}

type stubProjects struct {
	list func(ctx context.Context, userID string) (ProjectListJSON, error)
}

func (s stubProjects) List(ctx context.Context, userID string) (ProjectListJSON, error) {
	return s.list(ctx, userID)
}

// TestExecList_SlimByDefault covers the payload-shaping contract: without
// verbose=true, only the five slim fields survive. The model uses this to keep
// prompt-token cost bounded across multi-turn read loops.
func TestExecList_SlimByDefault(t *testing.T) {
	listPayload := map[string]any{
		"items": []map[string]any{
			{
				"id": "t1", "title": "Buy milk", "status": "active",
				"projectId": "p1", "scheduledFor": "2026-08-10",
				"notes": "very much notes", "priority": "high", "energy": "low",
			},
		},
	}
	exec := NewToolExecutor(
		stubTasks{listForUsr: func(context.Context, string, UserListInput) (TaskListJSON, error) {
			return TaskListJSON{Value: listPayload}, nil
		}},
		stubProjects{},
	)
	raw, err := exec.ExecList(context.Background(), "u1", json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var got struct {
		Items []map[string]any `json:"items"`
	}
	if uerr := json.Unmarshal(raw, &got); uerr != nil {
		t.Fatalf("bad json: %v", uerr)
	}
	if len(got.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(got.Items))
	}
	if _, present := got.Items[0]["notes"]; present {
		t.Error("slim payload must not carry notes")
	}
	if got.Items[0]["title"] != "Buy milk" {
		t.Errorf("title = %v, want Buy milk", got.Items[0]["title"])
	}
}

// TestExecList_VerbosePassesThrough — verbose:true should skip the slim
// projection and pass the full list value through unchanged.
func TestExecList_VerbosePassesThrough(t *testing.T) {
	listPayload := map[string]any{
		"items": []map[string]any{{"id": "t1", "title": "T", "notes": "keep me"}},
	}
	exec := NewToolExecutor(
		stubTasks{listForUsr: func(context.Context, string, UserListInput) (TaskListJSON, error) {
			return TaskListJSON{Value: listPayload}, nil
		}},
		stubProjects{},
	)
	raw, err := exec.ExecList(context.Background(), "u1", json.RawMessage(`{"verbose":true}`))
	if err != nil {
		t.Fatal(err)
	}
	var back struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(raw, &back)
	if back.Items[0]["notes"] != "keep me" {
		t.Errorf("verbose must keep notes; got %v", back.Items[0])
	}
}

// TestExecGet_MissingTaskIDInvalid — bad input never reaches the task service.
func TestExecGet_MissingTaskIDInvalid(t *testing.T) {
	exec := NewToolExecutor(stubTasks{}, stubProjects{})
	_, err := exec.ExecGet(context.Background(), "u1", json.RawMessage(`{}`))
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Code != apperror.ErrInvalidInput {
		t.Fatalf("want INVALID_INPUT, got %v", err)
	}
}

// TestExecGet_UnknownTaskBubbles — apperror from task service surfaces intact.
func TestExecGet_UnknownTaskBubbles(t *testing.T) {
	notFound := apperror.New(http.StatusNotFound, apperror.ErrTaskNotFound, "task not found")
	exec := NewToolExecutor(
		stubTasks{get: func(context.Context, string, string) (TaskViewJSON, error) {
			return TaskViewJSON{}, notFound
		}},
		stubProjects{},
	)
	_, err := exec.ExecGet(context.Background(), "u1", json.RawMessage(`{"taskId":"foreign"}`))
	if !errors.Is(err, notFound) {
		t.Fatalf("apperror must pass through unchanged; got %v", err)
	}
}

// TestExecCreate_MissingProjectID — required-field validation before any DB call.
func TestExecCreate_MissingProjectID(t *testing.T) {
	exec := NewToolExecutor(stubTasks{}, stubProjects{})
	_, err := exec.ExecCreate(context.Background(), "u1", "free", json.RawMessage(`{"title":"x"}`))
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Code != apperror.ErrInvalidInput {
		t.Fatalf("want INVALID_INPUT for missing projectId; got %v", err)
	}
}

// TestExecCreate_ProjectNotFound — task service PROJECT_NOT_FOUND bubbles.
func TestExecCreate_ProjectNotFound(t *testing.T) {
	pnf := apperror.New(http.StatusNotFound, apperror.ErrProjectNotFound, "project not found")
	exec := NewToolExecutor(
		stubTasks{create: func(context.Context, string, string, string, CreateTaskInput) (TaskViewJSON, error) {
			return TaskViewJSON{}, pnf
		}},
		stubProjects{},
	)
	_, err := exec.ExecCreate(context.Background(), "u1", "free",
		json.RawMessage(`{"projectId":"missing","title":"buy milk"}`))
	if !errors.Is(err, pnf) {
		t.Fatalf("want PROJECT_NOT_FOUND, got %v", err)
	}
}

// TestExecCreate_FreePlanLimitBubbles — the underlying plan-limit check must
// surface unchanged so the model can explain it to the user.
func TestExecCreate_FreePlanLimitBubbles(t *testing.T) {
	limit := apperror.New(http.StatusForbidden, apperror.ErrPlanLimitExceeded, "free plan allows up to 50 active tasks per project")
	exec := NewToolExecutor(
		stubTasks{create: func(context.Context, string, string, string, CreateTaskInput) (TaskViewJSON, error) {
			return TaskViewJSON{}, limit
		}},
		stubProjects{},
	)
	_, err := exec.ExecCreate(context.Background(), "u1", "free",
		json.RawMessage(`{"projectId":"p1","title":"buy milk"}`))
	if !errors.Is(err, limit) {
		t.Fatalf("plan-limit must bubble, got %v", err)
	}
}

// TestExecComplete_TranslatesToSetStatusDone — verifies the tool routes to the
// idiomatic status='done' PATCH rather than a bespoke code path.
func TestExecComplete_TranslatesToSetStatusDone(t *testing.T) {
	var gotStatus string
	exec := NewToolExecutor(
		stubTasks{setStatus: func(_ context.Context, _, _, _, status string) (TaskViewJSON, error) {
			gotStatus = status
			return TaskViewJSON{Value: map[string]any{"id": "t1", "status": status}}, nil
		}},
		stubProjects{},
	)
	_, err := exec.ExecComplete(context.Background(), "u1", "free", json.RawMessage(`{"taskId":"t1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if gotStatus != "done" {
		t.Errorf("complete must set status=done; got %q", gotStatus)
	}
}

// TestEncodeExecErr_Envelope — apperror is preserved; a bare error becomes an
// internal-server envelope with no Go text leaking to the model.
func TestEncodeExecErr_Envelope(t *testing.T) {
	ae := apperror.New(http.StatusNotFound, apperror.ErrTaskNotFound, "task not found")
	got := EncodeExecErr(ae)
	if got == "" || got == "null" {
		t.Fatal("empty envelope")
	}
	var env ToolResultEnvelope
	if err := json.Unmarshal([]byte(got), &env); err != nil {
		t.Fatalf("bad json: %v", err)
	}
	if env.Code != apperror.ErrTaskNotFound {
		t.Errorf("code = %q, want TASK_NOT_FOUND", env.Code)
	}

	generic := EncodeExecErr(errors.New("some internal driver text"))
	var g2 ToolResultEnvelope
	_ = json.Unmarshal([]byte(generic), &g2)
	if g2.Code != apperror.ErrInternalServerError {
		t.Errorf("bare errors must collapse to INTERNAL_SERVER_ERROR, got %q", g2.Code)
	}
	if g2.Message == "some internal driver text" {
		t.Error("must not leak the raw Go error text to the model")
	}
}
