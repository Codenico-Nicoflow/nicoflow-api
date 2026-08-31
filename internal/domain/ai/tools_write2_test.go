package ai

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// ── stub seams (one field per method actually exercised, matching the
//    stubTasks/stubProjects pattern in tools_service_test.go) ───────────────

type stubRecurrence struct {
	create    func(ctx context.Context, userID, projectID, plan string, req RuleCreateInput) (RuleViewJSON, error)
	convert   func(ctx context.Context, userID, taskID, plan string, req RuleCreateInput) (RuleViewJSON, error)
	update    func(ctx context.Context, userID, id, plan string, req RuleUpdateInput) (RuleViewJSON, error)
	setPaused func(ctx context.Context, userID, id, plan string, paused bool) (RuleViewJSON, error)
	deleteFn  func(ctx context.Context, userID, id string) error
}

func (s stubRecurrence) Create(ctx context.Context, userID, projectID, plan string, req RuleCreateInput) (RuleViewJSON, error) {
	return s.create(ctx, userID, projectID, plan, req)
}
func (s stubRecurrence) ConvertToRecurring(ctx context.Context, userID, taskID, plan string, req RuleCreateInput) (RuleViewJSON, error) {
	return s.convert(ctx, userID, taskID, plan, req)
}
func (s stubRecurrence) Update(ctx context.Context, userID, id, plan string, req RuleUpdateInput) (RuleViewJSON, error) {
	return s.update(ctx, userID, id, plan, req)
}
func (s stubRecurrence) SetPaused(ctx context.Context, userID, id, plan string, paused bool) (RuleViewJSON, error) {
	return s.setPaused(ctx, userID, id, plan, paused)
}
func (s stubRecurrence) Delete(ctx context.Context, userID, id string) error {
	return s.deleteFn(ctx, userID, id)
}

type stubNotes struct {
	create func(ctx context.Context, userID, projectID, title string, content json.RawMessage) (NoteViewJSON, error)
}

func (s stubNotes) Create(ctx context.Context, userID, projectID, title string, content json.RawMessage) (NoteViewJSON, error) {
	return s.create(ctx, userID, projectID, title, content)
}

type stubProjectMgmt struct {
	create func(ctx context.Context, userID, areaID, plan string, req ProjectCreateInput) (ProjectViewJSON, error)
	update func(ctx context.Context, userID, id string, req ProjectUpdateInput) (ProjectViewJSON, error)
}

func (s stubProjectMgmt) Create(ctx context.Context, userID, areaID, plan string, req ProjectCreateInput) (ProjectViewJSON, error) {
	return s.create(ctx, userID, areaID, plan, req)
}
func (s stubProjectMgmt) Update(ctx context.Context, userID, id string, req ProjectUpdateInput) (ProjectViewJSON, error) {
	return s.update(ctx, userID, id, req)
}

type stubAreas struct {
	create func(ctx context.Context, userID, plan, name, color, icon string) (AreaViewJSON, error)
}

func (s stubAreas) Create(ctx context.Context, userID, plan, name, color, icon string) (AreaViewJSON, error) {
	return s.create(ctx, userID, plan, name, color, icon)
}

type stubSubtasks struct {
	add     func(ctx context.Context, userID, taskID, title string) (SubtaskViewJSON, error)
	setDone func(ctx context.Context, userID, taskID, subtaskID string, done bool) (SubtaskViewJSON, error)
}

func (s stubSubtasks) Add(ctx context.Context, userID, taskID, title string) (SubtaskViewJSON, error) {
	return s.add(ctx, userID, taskID, title)
}
func (s stubSubtasks) SetDone(ctx context.Context, userID, taskID, subtaskID string, done bool) (SubtaskViewJSON, error) {
	return s.setDone(ctx, userID, taskID, subtaskID, done)
}

type stubBuckets struct {
	process func(ctx context.Context, userID, id, plan string, req BucketProcessInput) (BucketViewJSON, error)
}

func (s stubBuckets) Process(ctx context.Context, userID, id, plan string, req BucketProcessInput) (BucketViewJSON, error) {
	return s.process(ctx, userID, id, plan, req)
}

// ── AvailableTools gating ────────────────────────────────────────────────

func TestAvailableTools_NilSeamsDisableTheirTools(t *testing.T) {
	exec := NewToolExecutor(stubTasks{}, stubProjects{})
	avail := exec.AvailableTools()
	for _, name := range []string{
		ToolSetupRecurringTask, ToolAdjustRecurringTask, ToolPauseRecurringTask, ToolEndRecurringSeries,
		ToolCreateNote, ToolCreateArea, ToolCreateProject, ToolUpdateProject,
		ToolAddSubtask, ToolCompleteSubtask, ToolProcessBucketItem,
	} {
		if avail[name] {
			t.Errorf("%s should be unavailable with no seam wired", name)
		}
	}

	full := exec.
		WithRecurrence(stubRecurrence{}).
		WithNotes(stubNotes{}).
		WithProjectMgmt(stubProjectMgmt{}).
		WithAreas(stubAreas{}).
		WithSubtasks(stubSubtasks{}).
		WithBuckets(stubBuckets{})
	avail = full.AvailableTools()
	for _, name := range []string{
		ToolSetupRecurringTask, ToolAdjustRecurringTask, ToolPauseRecurringTask, ToolEndRecurringSeries,
		ToolCreateNote, ToolCreateArea, ToolCreateProject, ToolUpdateProject,
		ToolAddSubtask, ToolCompleteSubtask, ToolProcessBucketItem,
	} {
		if !avail[name] {
			t.Errorf("%s should be available once its seam is wired", name)
		}
	}
}

func TestExecSetupRecurring_UnwiredReturnsAIUnavailable(t *testing.T) {
	exec := NewToolExecutor(stubTasks{}, stubProjects{})
	_, err := exec.ExecSetupRecurring(context.Background(), "u1", "free", json.RawMessage(`{}`))
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Code != apperror.ErrAIUnavailable {
		t.Fatalf("want AI_UNAVAILABLE, got %v", err)
	}
}

// ── confirm-dispatch: one test per new tool, correct service + args. Split
//    into separate top-level funcs (rather than one t.Run cascade) purely to
//    keep gocyclo happy — each case is independent and gains nothing from
//    being subtests of a shared parent. ─────────────────────────────────────

func TestConfirmDispatch_SetupRecurringCreatesFromScratch(t *testing.T) {
	var gotProjectID, gotPlan string
	exec := NewToolExecutor(stubTasks{}, stubProjects{}).WithRecurrence(stubRecurrence{
		create: func(_ context.Context, _, projectID, plan string, req RuleCreateInput) (RuleViewJSON, error) {
			gotProjectID, gotPlan = projectID, plan
			return RuleViewJSON{Value: map[string]any{"id": "r1", "freq": req.Freq}}, nil
		},
	})
	raw, err := exec.ExecSetupRecurring(context.Background(), "u1", "pro",
		json.RawMessage(`{"projectId":"p1","title":"Water plants","freq":"weekly","interval":1,"startDate":"2026-09-01"}`))
	if err != nil {
		t.Fatal(err)
	}
	if gotProjectID != "p1" || gotPlan != "pro" {
		t.Errorf("routed with projectId=%q plan=%q, want p1/pro", gotProjectID, gotPlan)
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if out["freq"] != "weekly" {
		t.Errorf("freq = %v, want weekly", out["freq"])
	}
}

func TestConfirmDispatch_SetupRecurringConvertsExistingTask(t *testing.T) {
	var gotTaskID string
	exec := NewToolExecutor(stubTasks{}, stubProjects{}).WithRecurrence(stubRecurrence{
		convert: func(_ context.Context, _, taskID, _ string, _ RuleCreateInput) (RuleViewJSON, error) {
			gotTaskID = taskID
			return RuleViewJSON{Value: map[string]any{"id": "r2"}}, nil
		},
	})
	_, err := exec.ExecSetupRecurring(context.Background(), "u1", "pro",
		json.RawMessage(`{"taskId":"t9","freq":"daily","interval":1,"startDate":"2026-09-01"}`))
	if err != nil {
		t.Fatal(err)
	}
	if gotTaskID != "t9" {
		t.Errorf("routed to ConvertToRecurring with taskId=%q, want t9", gotTaskID)
	}
}

func TestConfirmDispatch_AdjustRecurringTriState(t *testing.T) {
	var got RuleUpdateInput
	exec := NewToolExecutor(stubTasks{}, stubProjects{}).WithRecurrence(stubRecurrence{
		update: func(_ context.Context, _, id, _ string, req RuleUpdateInput) (RuleViewJSON, error) {
			got = req
			return RuleViewJSON{Value: map[string]any{"id": id}}, nil
		},
	})
	_, err := exec.ExecAdjustRecurring(context.Background(), "u1", "pro",
		json.RawMessage(`{"ruleId":"r1","notes":null,"interval":2}`))
	if err != nil {
		t.Fatal(err)
	}
	if !got.NotesSet || got.Notes != nil {
		t.Errorf("explicit null notes must decode to Set=true Value=nil, got %+v", got)
	}
	if got.Interval == nil || *got.Interval != 2 {
		t.Errorf("interval = %v, want 2", got.Interval)
	}
}

func TestConfirmDispatch_PauseRecurringRoutesToSetPaused(t *testing.T) {
	var gotPaused bool
	exec := NewToolExecutor(stubTasks{}, stubProjects{}).WithRecurrence(stubRecurrence{
		setPaused: func(_ context.Context, _, _, _ string, paused bool) (RuleViewJSON, error) {
			gotPaused = paused
			return RuleViewJSON{Value: map[string]any{}}, nil
		},
	})
	_, err := exec.ExecPauseRecurring(context.Background(), "u1", "pro", json.RawMessage(`{"ruleId":"r1","paused":true}`))
	if err != nil {
		t.Fatal(err)
	}
	if !gotPaused {
		t.Error("paused must be true")
	}
}

func TestConfirmDispatch_EndRecurringSeriesRoutesToDelete(t *testing.T) {
	var gotID string
	exec := NewToolExecutor(stubTasks{}, stubProjects{}).WithRecurrence(stubRecurrence{
		deleteFn: func(_ context.Context, _, id string) error { gotID = id; return nil },
	})
	_, err := exec.ExecEndRecurringSeries(context.Background(), "u1", json.RawMessage(`{"ruleId":"r1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if gotID != "r1" {
		t.Errorf("routed Delete with id=%q, want r1", gotID)
	}
}

func TestConfirmDispatch_CreateNoteConvertsBlocks(t *testing.T) {
	var gotProjectID, gotTitle string
	var gotContent json.RawMessage
	exec := NewToolExecutor(stubTasks{}, stubProjects{}).WithNotes(stubNotes{
		create: func(_ context.Context, _, projectID, title string, content json.RawMessage) (NoteViewJSON, error) {
			gotProjectID, gotTitle, gotContent = projectID, title, content
			return NoteViewJSON{Value: map[string]any{"id": "n1", "title": title}}, nil
		},
	})
	raw, err := exec.ExecCreateNote(context.Background(), "u1",
		json.RawMessage(`{"projectId":"p1","title":"Meeting notes","blocks":[{"kind":"paragraph","text":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if gotProjectID != "p1" || gotTitle != "Meeting notes" {
		t.Errorf("routed with projectId=%q title=%q", gotProjectID, gotTitle)
	}
	if len(gotContent) == 0 {
		t.Error("expected a converted ProseMirror doc, got empty content")
	}
	var out map[string]any
	_ = json.Unmarshal(raw, &out)
	if _, ok := out["preview"]; !ok {
		t.Error("create_note result must include a preview field")
	}
}

func TestConfirmDispatch_CreateAreaRoutesFields(t *testing.T) {
	var gotName, gotColor, gotIcon string
	exec := NewToolExecutor(stubTasks{}, stubProjects{}).WithAreas(stubAreas{
		create: func(_ context.Context, _, _, name, color, icon string) (AreaViewJSON, error) {
			gotName, gotColor, gotIcon = name, color, icon
			return AreaViewJSON{Value: map[string]any{"id": "a1"}}, nil
		},
	})
	_, err := exec.ExecCreateArea(context.Background(), "u1", "free",
		json.RawMessage(`{"name":"Health","color":"#3B82F6","icon":"heart"}`))
	if err != nil {
		t.Fatal(err)
	}
	if gotName != "Health" || gotColor != "#3B82F6" || gotIcon != "heart" {
		t.Errorf("got name=%q color=%q icon=%q", gotName, gotColor, gotIcon)
	}
}

func TestConfirmDispatch_CreateProjectRoutesAreaAndName(t *testing.T) {
	var gotAreaID string
	var gotReq ProjectCreateInput
	exec := NewToolExecutor(stubTasks{}, stubProjects{}).WithProjectMgmt(stubProjectMgmt{
		create: func(_ context.Context, _, areaID, _ string, req ProjectCreateInput) (ProjectViewJSON, error) {
			gotAreaID, gotReq = areaID, req
			return ProjectViewJSON{Value: map[string]any{"id": "p1"}}, nil
		},
	})
	_, err := exec.ExecCreateProject(context.Background(), "u1", "free",
		json.RawMessage(`{"areaId":"a1","name":"Renovation","folderIcon":"hammer"}`))
	if err != nil {
		t.Fatal(err)
	}
	if gotAreaID != "a1" || gotReq.Name != "Renovation" || gotReq.FolderIcon != "hammer" {
		t.Errorf("got areaId=%q req=%+v", gotAreaID, gotReq)
	}
}

func TestConfirmDispatch_UpdateProjectDescriptionTriState(t *testing.T) {
	var gotReq ProjectUpdateInput
	exec := NewToolExecutor(stubTasks{}, stubProjects{}).WithProjectMgmt(stubProjectMgmt{
		update: func(_ context.Context, _, _ string, req ProjectUpdateInput) (ProjectViewJSON, error) {
			gotReq = req
			return ProjectViewJSON{Value: map[string]any{"id": "p1"}}, nil
		},
	})
	_, err := exec.ExecUpdateProject(context.Background(), "u1", json.RawMessage(`{"projectId":"p1","description":""}`))
	if err != nil {
		t.Fatal(err)
	}
	if !gotReq.DescSet || gotReq.Description == nil || *gotReq.Description != "" {
		t.Errorf("expected DescSet=true Description=\"\", got %+v", gotReq)
	}
}

func TestConfirmDispatch_AddSubtaskRoutesTaskAndTitle(t *testing.T) {
	var gotTaskID, gotTitle string
	exec := NewToolExecutor(stubTasks{}, stubProjects{}).WithSubtasks(stubSubtasks{
		add: func(_ context.Context, _, taskID, title string) (SubtaskViewJSON, error) {
			gotTaskID, gotTitle = taskID, title
			return SubtaskViewJSON{Value: map[string]any{"id": "s1"}}, nil
		},
	})
	_, err := exec.ExecAddSubtask(context.Background(), "u1", json.RawMessage(`{"taskId":"t1","title":"Buy paint"}`))
	if err != nil {
		t.Fatal(err)
	}
	if gotTaskID != "t1" || gotTitle != "Buy paint" {
		t.Errorf("got taskId=%q title=%q", gotTaskID, gotTitle)
	}
}

func TestConfirmDispatch_CompleteSubtaskRoutesSetDoneTrue(t *testing.T) {
	var gotDone bool
	exec := NewToolExecutor(stubTasks{}, stubProjects{}).WithSubtasks(stubSubtasks{
		setDone: func(_ context.Context, _, _, _ string, done bool) (SubtaskViewJSON, error) {
			gotDone = done
			return SubtaskViewJSON{Value: map[string]any{"id": "s1", "done": done}}, nil
		},
	})
	_, err := exec.ExecCompleteSubtask(context.Background(), "u1", json.RawMessage(`{"taskId":"t1","subtaskId":"s1"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !gotDone {
		t.Error("complete_subtask must call SetDone(true)")
	}
}

func TestConfirmDispatch_ProcessBucketItemTaskPath(t *testing.T) {
	var gotReq BucketProcessInput
	exec := NewToolExecutor(stubTasks{}, stubProjects{}).WithBuckets(stubBuckets{
		process: func(_ context.Context, _, _, _ string, req BucketProcessInput) (BucketViewJSON, error) {
			gotReq = req
			return BucketViewJSON{Value: map[string]any{"id": "b1"}}, nil
		},
	})
	_, err := exec.ExecProcessBucketItem(context.Background(), "u1", "free",
		json.RawMessage(`{"bucketId":"b1","processingResult":"task","projectId":"p1","taskDetails":{"title":"Do thing"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if gotReq.TaskTitle != "Do thing" || gotReq.ProcessingResult != "task" {
		t.Errorf("got %+v", gotReq)
	}
}

func TestConfirmDispatch_ProcessBucketItemNotePathNeedsNotesWired(t *testing.T) {
	exec := NewToolExecutor(stubTasks{}, stubProjects{}).WithBuckets(stubBuckets{
		process: func(_ context.Context, _, _, _ string, _ BucketProcessInput) (BucketViewJSON, error) {
			t.Fatal("Process must not be called when notes seam is missing")
			return BucketViewJSON{}, nil
		},
	})
	_, err := exec.ExecProcessBucketItem(context.Background(), "u1", "free",
		json.RawMessage(`{"bucketId":"b1","processingResult":"note","projectId":"p1","noteDetails":{"title":"N","blocks":[{"kind":"paragraph","text":"hi"}]}}`))
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Code != apperror.ErrAIUnavailable {
		t.Fatalf("want AI_UNAVAILABLE, got %v", err)
	}
}

func TestConfirmDispatch_ProcessBucketItemNotePathWithNotesWired(t *testing.T) {
	var gotContent json.RawMessage
	exec := NewToolExecutor(stubTasks{}, stubProjects{}).
		WithBuckets(stubBuckets{
			process: func(_ context.Context, _, _, _ string, req BucketProcessInput) (BucketViewJSON, error) {
				gotContent = req.NoteContent
				return BucketViewJSON{Value: map[string]any{"id": "b1"}}, nil
			},
		}).
		WithNotes(stubNotes{
			create: func(context.Context, string, string, string, json.RawMessage) (NoteViewJSON, error) {
				t.Fatal("bucket note path calls buckets.Process directly, not notes.Create")
				return NoteViewJSON{}, nil
			},
		})
	_, err := exec.ExecProcessBucketItem(context.Background(), "u1", "free",
		json.RawMessage(`{"bucketId":"b1","processingResult":"note","projectId":"p1","noteDetails":{"title":"N","blocks":[{"kind":"paragraph","text":"hi"}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(gotContent) == 0 {
		t.Error("expected a converted ProseMirror doc passed to bucket.Process")
	}
}

func TestConfirmDispatch_ProcessBucketItemTrashPathNeedsNoSubDetail(t *testing.T) {
	exec := NewToolExecutor(stubTasks{}, stubProjects{}).WithBuckets(stubBuckets{
		process: func(_ context.Context, _, _, _ string, _ BucketProcessInput) (BucketViewJSON, error) {
			return BucketViewJSON{Value: map[string]any{"id": "b1"}}, nil
		},
	})
	_, err := exec.ExecProcessBucketItem(context.Background(), "u1", "free",
		json.RawMessage(`{"bucketId":"b1","processingResult":"trash"}`))
	if err != nil {
		t.Fatal(err)
	}
}

// ── execWriteTool dispatch (stream.go's switch) sanity: unknown tool errors ─

func TestEncodeExecErr_PreservesAIUnavailable(t *testing.T) {
	err := errAIUnavailable()
	env := EncodeExecErr(err)
	var got ToolResultEnvelope
	if uerr := json.Unmarshal([]byte(env), &got); uerr != nil {
		t.Fatalf("bad json: %v", uerr)
	}
	if got.Code != apperror.ErrAIUnavailable {
		t.Errorf("code = %q, want AI_UNAVAILABLE", got.Code)
	}
}
