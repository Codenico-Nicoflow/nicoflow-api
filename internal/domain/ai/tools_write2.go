package ai

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// This file holds the Exec implementations for the 11 NIC-1997 write tools —
// split from tools_service.go purely to keep that file to the original six
// tools. Same pattern throughout: decode args → validate required fields →
// translate onto the target seam's *Input shape → re-marshal the result.

func errAIUnavailable() error {
	return apperror.New(http.StatusServiceUnavailable, apperror.ErrAIUnavailable, "this tool is not enabled")
}

func invalidArgs(msg string) error {
	return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, msg)
}

// ── setup_recurring_task / adjust_recurring_task / pause_recurring_task /
//    end_recurring_series ──────────────────────────────────────────────────

type setupRecurringArgs struct {
	TaskID           *string `json:"taskId,omitempty"`
	ProjectID        string  `json:"projectId,omitempty"`
	Title            string  `json:"title,omitempty"`
	Notes            *string `json:"notes,omitempty"`
	Priority         string  `json:"priority,omitempty"`
	Energy           string  `json:"energy,omitempty"`
	EstimatedMinutes *int    `json:"estimatedMinutes,omitempty"`
	Freq             string  `json:"freq"`
	Interval         int     `json:"interval"`
	ByWeekday        []int   `json:"byWeekday,omitempty"`
	ByMonthday       *int    `json:"byMonthday,omitempty"`
	StartDate        string  `json:"startDate"`
	EndDate          *string `json:"endDate,omitempty"`
	ScheduledTime    *string `json:"scheduledTime,omitempty"`
}

func (a setupRecurringArgs) toRuleCreateInput() RuleCreateInput {
	return RuleCreateInput{
		Title: a.Title, Notes: a.Notes, Priority: a.Priority, Energy: a.Energy,
		EstimatedMinutes: a.EstimatedMinutes, ScheduledTime: a.ScheduledTime,
		Freq: a.Freq, Interval: a.Interval, ByWeekday: a.ByWeekday, ByMonthday: a.ByMonthday,
		StartDate: a.StartDate, EndDate: a.EndDate,
	}
}

func (e *toolExecutor) ExecSetupRecurring(ctx context.Context, userID, plan string, input json.RawMessage) (json.RawMessage, error) {
	if e.recurrence == nil {
		return nil, errAIUnavailable()
	}
	var args setupRecurringArgs
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, invalidArgs("invalid setup_recurring_task arguments")
	}
	if args.Freq == "" || args.Interval <= 0 || args.StartDate == "" {
		return nil, invalidArgs("freq, interval and startDate are required")
	}
	if args.TaskID != nil && *args.TaskID != "" {
		v, err := e.recurrence.ConvertToRecurring(ctx, userID, *args.TaskID, plan, args.toRuleCreateInput())
		if err != nil {
			return nil, err
		}
		return json.Marshal(v.Value)
	}
	if args.ProjectID == "" || args.Title == "" {
		return nil, invalidArgs("projectId and title are required when taskId is absent")
	}
	v, err := e.recurrence.Create(ctx, userID, args.ProjectID, plan, args.toRuleCreateInput())
	if err != nil {
		return nil, err
	}
	return json.Marshal(v.Value)
}

type adjustRecurringArgs struct {
	RuleID           string          `json:"ruleId"`
	Title            *string         `json:"title,omitempty"`
	Notes            json.RawMessage `json:"notes,omitempty"`
	Priority         *string         `json:"priority,omitempty"`
	Energy           *string         `json:"energy,omitempty"`
	EstimatedMinutes json.RawMessage `json:"estimatedMinutes,omitempty"`
	ScheduledTime    json.RawMessage `json:"scheduledTime,omitempty"`
	Freq             *string         `json:"freq,omitempty"`
	Interval         *int            `json:"interval,omitempty"`
	ByWeekday        *[]int          `json:"byWeekday,omitempty"`
	ByMonthday       json.RawMessage `json:"byMonthday,omitempty"`
	StartDate        *string         `json:"startDate,omitempty"`
	EndDate          json.RawMessage `json:"endDate,omitempty"`
}

// decodeTriState reads an optionally-present field that also accepts explicit
// JSON null (to clear it) — mirrors optional.Field's Set/Value semantics
// without ai importing pkg/optional's generic type directly in an args struct.
//
// raw must be a plain (non-pointer) json.RawMessage field: encoding/json sets
// a *json.RawMessage to nil on an explicit `null`, which is indistinguishable
// from the key being absent altogether — the non-pointer field instead decodes
// null into the literal bytes "null", which is exactly the signal this
// function needs to tell "absent" (raw == nil) from "explicitly cleared"
// (raw non-nil, string(raw) == "null") apart.
func decodeTriState[T any](raw json.RawMessage) (set bool, value *T, err error) {
	if raw == nil {
		return false, nil, nil
	}
	if string(raw) == "null" {
		return true, nil, nil
	}
	var v T
	if err := json.Unmarshal(raw, &v); err != nil {
		return false, nil, err
	}
	return true, &v, nil
}

func (a adjustRecurringArgs) toRuleUpdateInput() (RuleUpdateInput, error) {
	out := RuleUpdateInput{
		Title: a.Title, Priority: a.Priority, Energy: a.Energy,
		Freq: a.Freq, Interval: a.Interval, ByWeekday: a.ByWeekday, StartDate: a.StartDate,
	}
	var err error
	if out.NotesSet, out.Notes, err = decodeTriState[string](a.Notes); err != nil {
		return out, invalidArgs("invalid notes value")
	}
	if out.EstimatedMinsSet, out.EstimatedMinutes, err = decodeTriState[int](a.EstimatedMinutes); err != nil {
		return out, invalidArgs("invalid estimatedMinutes value")
	}
	if out.ScheduledTimeSet, out.ScheduledTime, err = decodeTriState[string](a.ScheduledTime); err != nil {
		return out, invalidArgs("invalid scheduledTime value")
	}
	if out.ByMonthdaySet, out.ByMonthday, err = decodeTriState[int](a.ByMonthday); err != nil {
		return out, invalidArgs("invalid byMonthday value")
	}
	if out.EndDateSet, out.EndDate, err = decodeTriState[string](a.EndDate); err != nil {
		return out, invalidArgs("invalid endDate value")
	}
	return out, nil
}

func (e *toolExecutor) ExecAdjustRecurring(ctx context.Context, userID, plan string, input json.RawMessage) (json.RawMessage, error) {
	if e.recurrence == nil {
		return nil, errAIUnavailable()
	}
	var args adjustRecurringArgs
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, invalidArgs("invalid adjust_recurring_task arguments")
	}
	if args.RuleID == "" {
		return nil, invalidArgs("ruleId is required")
	}
	upd, err := args.toRuleUpdateInput()
	if err != nil {
		return nil, err
	}
	v, err := e.recurrence.Update(ctx, userID, args.RuleID, plan, upd)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v.Value)
}

type pauseRecurringArgs struct {
	RuleID string `json:"ruleId"`
	Paused bool   `json:"paused"`
}

func (e *toolExecutor) ExecPauseRecurring(ctx context.Context, userID, plan string, input json.RawMessage) (json.RawMessage, error) {
	if e.recurrence == nil {
		return nil, errAIUnavailable()
	}
	var args pauseRecurringArgs
	if err := json.Unmarshal(input, &args); err != nil || args.RuleID == "" {
		return nil, invalidArgs("ruleId is required")
	}
	v, err := e.recurrence.SetPaused(ctx, userID, args.RuleID, plan, args.Paused)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v.Value)
}

type endRecurringSeriesArgs struct {
	RuleID string `json:"ruleId"`
}

func (e *toolExecutor) ExecEndRecurringSeries(ctx context.Context, userID string, input json.RawMessage) (json.RawMessage, error) {
	if e.recurrence == nil {
		return nil, errAIUnavailable()
	}
	var args endRecurringSeriesArgs
	if err := json.Unmarshal(input, &args); err != nil || args.RuleID == "" {
		return nil, invalidArgs("ruleId is required")
	}
	if err := e.recurrence.Delete(ctx, userID, args.RuleID); err != nil {
		return nil, err
	}
	return json.Marshal(map[string]string{"id": args.RuleID, "outcome": "series_ended"})
}

// ── create_note ──────────────────────────────────────────────────────────

type createNoteArgs struct {
	ProjectID string      `json:"projectId"`
	Title     string      `json:"title"`
	Blocks    []NoteBlock `json:"blocks"`
}

func (e *toolExecutor) ExecCreateNote(ctx context.Context, userID string, input json.RawMessage) (json.RawMessage, error) {
	if e.notes == nil {
		return nil, errAIUnavailable()
	}
	var args createNoteArgs
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, invalidArgs("invalid create_note arguments")
	}
	if args.ProjectID == "" || args.Title == "" {
		return nil, invalidArgs("projectId and title are required")
	}
	doc, err := blocksToProseMirror(args.Blocks)
	if err != nil {
		return nil, err
	}
	v, err := e.notes.Create(ctx, userID, args.ProjectID, args.Title, doc)
	if err != nil {
		return nil, err
	}
	return marshalWithPreview(v.Value, doc)
}

// marshalWithPreview wraps a created/proposed value together with its
// rendered Tiptap doc under "preview" so the frontend can show the note body
// without a second round-trip — required by the create_note / note branch of
// process_bucket_item's confirm-card contract.
func marshalWithPreview(value any, preview json.RawMessage) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var merged map[string]json.RawMessage
	if err := json.Unmarshal(raw, &merged); err != nil {
		// Non-object value (shouldn't happen for our view types) — fall back
		// to a wrapper rather than losing the preview.
		return json.Marshal(map[string]json.RawMessage{"result": raw, "preview": preview})
	}
	merged["preview"] = preview
	return json.Marshal(merged)
}

// ── create_area ──────────────────────────────────────────────────────────

type createAreaArgs struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
	Icon  string `json:"icon,omitempty"`
}

func (e *toolExecutor) ExecCreateArea(ctx context.Context, userID, plan string, input json.RawMessage) (json.RawMessage, error) {
	if e.areas == nil {
		return nil, errAIUnavailable()
	}
	var args createAreaArgs
	if err := json.Unmarshal(input, &args); err != nil || args.Name == "" {
		return nil, invalidArgs("name is required")
	}
	v, err := e.areas.Create(ctx, userID, plan, args.Name, args.Color, args.Icon)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v.Value)
}

// ── create_project / update_project ─────────────────────────────────────

type createProjectArgs struct {
	AreaID      string  `json:"areaId"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	FolderIcon  string  `json:"folderIcon,omitempty"`
}

func (e *toolExecutor) ExecCreateProject(ctx context.Context, userID, plan string, input json.RawMessage) (json.RawMessage, error) {
	if e.projectMgmt == nil {
		return nil, errAIUnavailable()
	}
	var args createProjectArgs
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, invalidArgs("invalid create_project arguments")
	}
	if args.AreaID == "" || args.Name == "" {
		return nil, invalidArgs("areaId and name are required")
	}
	v, err := e.projectMgmt.Create(ctx, userID, args.AreaID, plan, ProjectCreateInput{
		Name: args.Name, FolderIcon: args.FolderIcon, Description: args.Description,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(v.Value)
}

type updateProjectArgs struct {
	ProjectID   string          `json:"projectId"`
	Name        *string         `json:"name,omitempty"`
	Description json.RawMessage `json:"description,omitempty"`
	AreaID      *string         `json:"areaId,omitempty"`
	FolderIcon  *string         `json:"folderIcon,omitempty"`
}

func (e *toolExecutor) ExecUpdateProject(ctx context.Context, userID string, input json.RawMessage) (json.RawMessage, error) {
	if e.projectMgmt == nil {
		return nil, errAIUnavailable()
	}
	var args updateProjectArgs
	if err := json.Unmarshal(input, &args); err != nil || args.ProjectID == "" {
		return nil, invalidArgs("projectId is required")
	}
	descSet, desc, err := decodeTriState[string](args.Description)
	if err != nil {
		return nil, invalidArgs("invalid description value")
	}
	v, err := e.projectMgmt.Update(ctx, userID, args.ProjectID, ProjectUpdateInput{
		Name: args.Name, AreaID: args.AreaID, FolderIcon: args.FolderIcon,
		DescSet: descSet, Description: desc,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(v.Value)
}

// ── add_subtask / complete_subtask ──────────────────────────────────────

type addSubtaskArgs struct {
	TaskID string `json:"taskId"`
	Title  string `json:"title"`
}

func (e *toolExecutor) ExecAddSubtask(ctx context.Context, userID string, input json.RawMessage) (json.RawMessage, error) {
	if e.subtasks == nil {
		return nil, errAIUnavailable()
	}
	var args addSubtaskArgs
	if err := json.Unmarshal(input, &args); err != nil || args.TaskID == "" || args.Title == "" {
		return nil, invalidArgs("taskId and title are required")
	}
	v, err := e.subtasks.Add(ctx, userID, args.TaskID, args.Title)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v.Value)
}

type completeSubtaskArgs struct {
	TaskID    string `json:"taskId"`
	SubtaskID string `json:"subtaskId"`
}

func (e *toolExecutor) ExecCompleteSubtask(ctx context.Context, userID string, input json.RawMessage) (json.RawMessage, error) {
	if e.subtasks == nil {
		return nil, errAIUnavailable()
	}
	var args completeSubtaskArgs
	if err := json.Unmarshal(input, &args); err != nil || args.TaskID == "" || args.SubtaskID == "" {
		return nil, invalidArgs("taskId and subtaskId are required")
	}
	v, err := e.subtasks.SetDone(ctx, userID, args.TaskID, args.SubtaskID, true)
	if err != nil {
		return nil, err
	}
	return json.Marshal(v.Value)
}

// ── process_bucket_item ─────────────────────────────────────────────────

type processBucketTaskDetails struct {
	Title        string  `json:"title"`
	Notes        *string `json:"notes,omitempty"`
	Priority     *string `json:"priority,omitempty"`
	Energy       *string `json:"energy,omitempty"`
	ScheduledFor *string `json:"scheduledFor,omitempty"`
}

type processBucketNoteDetails struct {
	Title  string      `json:"title"`
	Blocks []NoteBlock `json:"blocks"`
}

type processBucketItemArgs struct {
	BucketID         string                    `json:"bucketId"`
	ProcessingResult string                    `json:"processingResult"`
	ProjectID        *string                   `json:"projectId,omitempty"`
	TaskDetails      *processBucketTaskDetails `json:"taskDetails,omitempty"`
	NoteDetails      *processBucketNoteDetails `json:"noteDetails,omitempty"`
}

func (e *toolExecutor) ExecProcessBucketItem(ctx context.Context, userID, plan string, input json.RawMessage) (json.RawMessage, error) {
	if e.buckets == nil {
		return nil, errAIUnavailable()
	}
	var args processBucketItemArgs
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, invalidArgs("invalid process_bucket_item arguments")
	}
	if args.BucketID == "" {
		return nil, invalidArgs("bucketId is required")
	}

	req, preview, err := e.processBucketRequest(args)
	if err != nil {
		return nil, err
	}

	v, err := e.buckets.Process(ctx, userID, args.BucketID, plan, req)
	if err != nil {
		return nil, err
	}
	if preview != nil {
		return marshalWithPreview(v.Value, preview)
	}
	return json.Marshal(v.Value)
}

// processBucketRequest builds the BucketProcessInput for one processingResult
// branch, split out of ExecProcessBucketItem purely to keep its cyclomatic
// complexity down. Returns a non-nil preview only for the note branch.
func (e *toolExecutor) processBucketRequest(args processBucketItemArgs) (BucketProcessInput, json.RawMessage, error) {
	req := BucketProcessInput{ProcessingResult: args.ProcessingResult, ProjectID: args.ProjectID}

	switch args.ProcessingResult {
	case "task":
		if args.ProjectID == nil || *args.ProjectID == "" || args.TaskDetails == nil {
			return req, nil, invalidArgs("projectId and taskDetails are required to process into a task")
		}
		req.TaskTitle = args.TaskDetails.Title
		req.TaskNotes = args.TaskDetails.Notes
		req.TaskPriority = args.TaskDetails.Priority
		req.TaskEnergy = args.TaskDetails.Energy
		req.TaskScheduledFor = args.TaskDetails.ScheduledFor
		return req, nil, nil
	case "note":
		return e.processBucketNoteRequest(args, req)
	case "trash":
		return req, nil, nil
	default:
		return req, nil, invalidArgs("processingResult must be one of: task, note, trash")
	}
}

func (e *toolExecutor) processBucketNoteRequest(args processBucketItemArgs, req BucketProcessInput) (BucketProcessInput, json.RawMessage, error) {
	if e.notes == nil {
		return req, nil, errAIUnavailable()
	}
	if args.ProjectID == nil || *args.ProjectID == "" || args.NoteDetails == nil {
		return req, nil, invalidArgs("projectId and noteDetails are required to process into a note")
	}
	doc, err := blocksToProseMirror(args.NoteDetails.Blocks)
	if err != nil {
		return req, nil, err
	}
	req.NoteTitle = args.NoteDetails.Title
	req.NoteContent = doc
	return req, doc, nil
}
