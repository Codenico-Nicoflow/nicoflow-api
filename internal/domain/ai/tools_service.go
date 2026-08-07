package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// ToolExecutor runs one AI tool call on the caller's own data and returns the
// JSON-encoded result. It lives in the ai package (the consumer) so the domain
// stays provider-agnostic; concretes wire it to the task/project services at
// startup — those services already own row-isolation, plan gates and WS emits.
//
// A returned error from any Exec* is a TOOL error (invalid input, missing task,
// plan-limit hit, DB failure); the caller wraps it as a tool_result with
// is_error=true so the model can explain conversationally, rather than the
// pipeline propagating an HTTP error to the user. Only a truly unrecoverable
// error (context cancelled, nil executor) escapes the executor at all.
type ToolExecutor interface {
	ExecList(ctx context.Context, userID string, input json.RawMessage) (json.RawMessage, error)
	ExecGet(ctx context.Context, userID string, input json.RawMessage) (json.RawMessage, error)
	ExecListProjects(ctx context.Context, userID string) (json.RawMessage, error)
	ExecComplete(ctx context.Context, userID, plan string, input json.RawMessage) (json.RawMessage, error)
	ExecReschedule(ctx context.Context, userID, plan string, input json.RawMessage) (json.RawMessage, error)
	ExecCreate(ctx context.Context, userID, plan string, input json.RawMessage) (json.RawMessage, error)
}

// TaskCommands is the narrow slice of the task service the executor calls into.
// Defined here so ai never imports task; the task service already implements
// every method, no adapter needed.
type TaskCommands interface {
	Get(ctx context.Context, userID, id string) (TaskViewJSON, error)
	Create(ctx context.Context, userID, projectID, plan string, req CreateTaskInput) (TaskViewJSON, error)
	SetStatus(ctx context.Context, userID, id, plan, status string) (TaskViewJSON, error)
	Schedule(ctx context.Context, userID, id, plan string, req ScheduleInput) (TaskViewJSON, error)
	ListForUser(ctx context.Context, userID string, f UserListInput) (TaskListJSON, error)
}

// ProjectCommands is the narrow slice of the project service the executor uses.
type ProjectCommands interface {
	List(ctx context.Context, userID string) (ProjectListJSON, error)
}

// TaskViewJSON / TaskListJSON / ProjectListJSON are opaque handles the concrete
// adapter constructs from the real domain shapes. The executor never inspects
// them — it just re-marshals through json.Marshal — so the ai package doesn't
// need to know the wire schema of another domain.
type (
	TaskViewJSON    struct{ Value any }
	TaskListJSON    struct{ Value any }
	ProjectListJSON struct{ Value any }
)

// CreateTaskInput mirrors task.CreateTaskRequest so the adapter can translate
// without ai importing task. Everything except projectId+title is optional.
type CreateTaskInput struct {
	Title            string
	Notes            *string
	Status           string
	Priority         string
	Energy           string
	RollsOver        *bool
	ScheduledFor     *string
	ScheduledTime    *string
	EstimatedMinutes *int
	URL              *string
}

// ScheduleInput mirrors task.ScheduleRequest.
type ScheduleInput struct {
	ScheduledFor  *string
	ScheduledTime *string
	RollsOver     *bool
}

// UserListInput mirrors task.UserListFilter.
type UserListInput struct {
	Status        *string
	Priority      *string
	Energy        *string
	ProjectID     *string
	ScheduledFrom *string
	ScheduledTo   *string
	Search        string
	Limit         int
}

// slimTaskView is the compact tool payload for list_tasks in non-verbose mode.
// Kept intentionally narrow — the model rarely needs the full 15-field TaskView
// to answer a question, and every extra field is prompt tokens on the next turn.
type slimTaskView struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Status       string  `json:"status"`
	ProjectID    string  `json:"projectId"`
	ScheduledFor *string `json:"scheduledFor,omitempty"`
}

// listTasksArgs decodes the list_tasks tool input.
type listTasksArgs struct {
	Status        *string `json:"status,omitempty"`
	Priority      *string `json:"priority,omitempty"`
	Energy        *string `json:"energy,omitempty"`
	ProjectID     *string `json:"projectId,omitempty"`
	ScheduledFrom *string `json:"scheduledFrom,omitempty"`
	ScheduledTo   *string `json:"scheduledTo,omitempty"`
	Search        string  `json:"search,omitempty"`
	Limit         int     `json:"limit,omitempty"`
	Verbose       bool    `json:"verbose,omitempty"`
}

type getTaskArgs struct {
	TaskID string `json:"taskId"`
}

type completeTaskArgs struct {
	TaskID string `json:"taskId"`
}

type rescheduleTaskArgs struct {
	TaskID        string  `json:"taskId"`
	ScheduledFor  *string `json:"scheduledFor,omitempty"`
	ScheduledTime *string `json:"scheduledTime,omitempty"`
	RollsOver     *bool   `json:"rollsOver,omitempty"`
}

type createTaskArgs struct {
	ProjectID        string  `json:"projectId"`
	Title            string  `json:"title"`
	Notes            *string `json:"notes,omitempty"`
	Status           string  `json:"status,omitempty"`
	Priority         string  `json:"priority,omitempty"`
	Energy           string  `json:"energy,omitempty"`
	RollsOver        *bool   `json:"rollsOver,omitempty"`
	ScheduledFor     *string `json:"scheduledFor,omitempty"`
	ScheduledTime    *string `json:"scheduledTime,omitempty"`
	EstimatedMinutes *int    `json:"estimatedMinutes,omitempty"`
	URL              *string `json:"url,omitempty"`
}

// toolExecutor is the concrete ToolExecutor wired at startup.
type toolExecutor struct {
	tasks    TaskCommands
	projects ProjectCommands
}

// NewToolExecutor builds the executor with its downstream services.
func NewToolExecutor(tasks TaskCommands, projects ProjectCommands) ToolExecutor {
	return &toolExecutor{tasks: tasks, projects: projects}
}

// slimTasks projects an ai-domain task list into the slim payload. The concrete
// adapter's TaskListJSON.Value is expected to be `[]task.TaskView` — we JSON
// round-trip so ai never imports the task package's exact type.
func slimTasks(list TaskListJSON) (json.RawMessage, error) {
	// Round-trip through JSON to reach the wire shape without importing task.
	raw, err := json.Marshal(list.Value)
	if err != nil {
		return nil, fmt.Errorf("marshal task list: %w", err)
	}
	// Peek at the items array only.
	var wrapper struct {
		Items []struct {
			ID           string  `json:"id"`
			Title        string  `json:"title"`
			Status       string  `json:"status"`
			ProjectID    string  `json:"projectId"`
			ScheduledFor *string `json:"scheduledFor"`
		} `json:"items"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, fmt.Errorf("decode task list: %w", err)
	}
	out := struct {
		Items []slimTaskView `json:"items"`
	}{Items: make([]slimTaskView, len(wrapper.Items))}
	for i, t := range wrapper.Items {
		out.Items[i] = slimTaskView{
			ID: t.ID, Title: t.Title, Status: t.Status,
			ProjectID: t.ProjectID, ScheduledFor: t.ScheduledFor,
		}
	}
	return json.Marshal(out)
}

func (e *toolExecutor) ExecList(ctx context.Context, userID string, input json.RawMessage) (json.RawMessage, error) {
	var args listTasksArgs
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			return nil, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "invalid list_tasks arguments")
		}
	}
	list, err := e.tasks.ListForUser(ctx, userID, UserListInput{
		Status: args.Status, Priority: args.Priority, Energy: args.Energy,
		ProjectID:     args.ProjectID,
		ScheduledFrom: args.ScheduledFrom, ScheduledTo: args.ScheduledTo,
		Search: args.Search, Limit: args.Limit,
	})
	if err != nil {
		return nil, err
	}
	if args.Verbose {
		return json.Marshal(list.Value)
	}
	return slimTasks(list)
}

func (e *toolExecutor) ExecGet(ctx context.Context, userID string, input json.RawMessage) (json.RawMessage, error) {
	var args getTaskArgs
	if err := json.Unmarshal(input, &args); err != nil || args.TaskID == "" {
		return nil, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "taskId is required")
	}
	view, err := e.tasks.Get(ctx, userID, args.TaskID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(view.Value)
}

func (e *toolExecutor) ExecListProjects(ctx context.Context, userID string) (json.RawMessage, error) {
	list, err := e.projects.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(list.Value)
}

func (e *toolExecutor) ExecComplete(ctx context.Context, userID, plan string, input json.RawMessage) (json.RawMessage, error) {
	var args completeTaskArgs
	if err := json.Unmarshal(input, &args); err != nil || args.TaskID == "" {
		return nil, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "taskId is required")
	}
	view, err := e.tasks.SetStatus(ctx, userID, args.TaskID, plan, "done")
	if err != nil {
		return nil, err
	}
	return json.Marshal(view.Value)
}

func (e *toolExecutor) ExecReschedule(ctx context.Context, userID, plan string, input json.RawMessage) (json.RawMessage, error) {
	var args rescheduleTaskArgs
	if err := json.Unmarshal(input, &args); err != nil || args.TaskID == "" {
		return nil, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "taskId is required")
	}
	view, err := e.tasks.Schedule(ctx, userID, args.TaskID, plan, ScheduleInput{
		ScheduledFor: args.ScheduledFor, ScheduledTime: args.ScheduledTime, RollsOver: args.RollsOver,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(view.Value)
}

func (e *toolExecutor) ExecCreate(ctx context.Context, userID, plan string, input json.RawMessage) (json.RawMessage, error) {
	var args createTaskArgs
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "invalid create_task arguments")
	}
	if args.ProjectID == "" {
		return nil, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "projectId is required")
	}
	if args.Title == "" {
		return nil, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "title is required")
	}
	view, err := e.tasks.Create(ctx, userID, args.ProjectID, plan, CreateTaskInput{
		Title: args.Title, Notes: args.Notes, Status: args.Status,
		Priority: args.Priority, Energy: args.Energy, RollsOver: args.RollsOver,
		ScheduledFor: args.ScheduledFor, ScheduledTime: args.ScheduledTime,
		EstimatedMinutes: args.EstimatedMinutes, URL: args.URL,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(view.Value)
}

// ToolResultEnvelope is what the executor writes into a tool_result on failure —
// a compact { code, message } the model can turn into user-facing text without
// dumping HTTP status codes at the reader.
type ToolResultEnvelope struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// EncodeExecErr renders an executor error as the tool_result payload we send
// back to Claude. AppError shapes get their typed code+message; anything else
// collapses to a generic INTERNAL_SERVER_ERROR — we never leak Go error text
// to the model.
func EncodeExecErr(err error) string {
	var ae *apperror.AppError
	env := ToolResultEnvelope{
		Code:    apperror.ErrInternalServerError,
		Message: "the tool failed to run",
	}
	if errors.As(err, &ae) {
		env.Code = ae.Code
		env.Message = ae.Message
	}
	b, mErr := json.Marshal(env)
	if mErr != nil {
		return `{"code":"INTERNAL_SERVER_ERROR","message":"the tool failed to run"}`
	}
	return string(b)
}
