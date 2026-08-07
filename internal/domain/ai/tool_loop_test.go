package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// ── shared test doubles for tool-loop scenarios ───────────────────────────────

// toolStream yields deltas and, on drain, exposes a pre-set list of ToolUses.
type toolStream struct {
	deltas   []string
	i        int
	toolUses []ToolUseBlock
	err      error
}

func (s *toolStream) Next() bool {
	if s.i >= len(s.deltas) {
		return false
	}
	s.i++
	return true
}
func (s *toolStream) Text() string             { return s.deltas[s.i-1] }
func (s *toolStream) Err() error               { return s.err }
func (s *toolStream) Usage() Usage             { return Usage{} }
func (s *toolStream) Close() error             { return nil }
func (s *toolStream) ToolUses() []ToolUseBlock { return s.toolUses }
func (s *toolStream) StopReason() string       { return "tool_use" }

// scriptedClient hands back a queued sequence of streams, one per assistant
// turn — that lets a test simulate a read-tool round-trip (turn 1 = tool_use,
// turn 2 = final answer text) with just two entries.
type scriptedClient struct {
	streams []Stream
	i       int
}

func (c *scriptedClient) Enabled() bool { return true }
func (c *scriptedClient) Stream(context.Context, ChatRequest) (Stream, error) {
	if c.i >= len(c.streams) {
		return &toolStream{}, nil
	}
	s := c.streams[c.i]
	c.i++
	return s, nil
}

// toolSink records everything sent through it.
type toolSink struct {
	deltas    []string
	proposals []toolPropCapture
}
type toolPropCapture struct {
	AssistantMessageID, ToolUseID, ToolName string
	Input                                   json.RawMessage
}

func (s *toolSink) Delta(text string) error { s.deltas = append(s.deltas, text); return nil }
func (s *toolSink) ToolProposal(aID, tID, tName string, input json.RawMessage) error {
	s.proposals = append(s.proposals, toolPropCapture{aID, tID, tName, input})
	return nil
}

// toolRepo is a tool-aware stubRepo tracking every write path we assert on.
type toolRepo struct {
	mu               sync.Mutex
	assistantAppends int
	toolResultsAppnd int
	pending          []ToolCall
	claimResult      ToolCall
	claimErr         error
}

func (r *toolRepo) CreateSession(context.Context, Session) (Session, error) { return Session{}, nil }
func (r *toolRepo) ListSessions(context.Context, string) ([]Session, error) { return nil, nil }
func (r *toolRepo) GetSession(_ context.Context, uid, id string) (*Session, error) {
	return &Session{ID: id, UserID: uid}, nil
}
func (r *toolRepo) ListMessages(context.Context, string) ([]SessionMessage, error) { return nil, nil }
func (r *toolRepo) DeleteSession(context.Context, string, string) error            { return nil }
func (r *toolRepo) UsageSum(context.Context, string) (int, error)                  { return 0, nil }
func (r *toolRepo) UsageForMonth(context.Context, string, string) (int, error)     { return 0, nil }
func (r *toolRepo) ReserveMonthly(context.Context, string, string, int) (string, error) {
	return "u", nil
}
func (r *toolRepo) ReserveLifetime(context.Context, string, string, int) (string, error) {
	return "u", nil
}
func (r *toolRepo) RefundUsage(context.Context, string) error { return nil }
func (r *toolRepo) AppendUserMessage(context.Context, string, string, string, string) error {
	return nil
}
func (r *toolRepo) AppendAssistantMessage(context.Context, string, string, string) error { return nil }
func (r *toolRepo) AppendAssistantMessageWithBlocks(context.Context, string, string, string, json.RawMessage) error {
	r.mu.Lock()
	r.assistantAppends++
	r.mu.Unlock()
	return nil
}
func (r *toolRepo) AppendToolResultsMessage(context.Context, string, string, json.RawMessage) error {
	r.mu.Lock()
	r.toolResultsAppnd++
	r.mu.Unlock()
	return nil
}
func (r *toolRepo) HistoryFor(context.Context, string, int) ([]SessionMessage, error) {
	return nil, nil
}
func (r *toolRepo) HistoryForWithBlocks(context.Context, string, int) ([]SessionMessage, error) {
	return nil, nil
}
func (r *toolRepo) PromptContext(context.Context, string) (PromptContext, error) {
	return PromptContext{Language: "en"}, nil
}
func (r *toolRepo) InsertPending(_ context.Context, tc ToolCall) error {
	r.mu.Lock()
	r.pending = append(r.pending, tc)
	r.mu.Unlock()
	return nil
}
func (r *toolRepo) CountForAssistantMessage(context.Context, string, string) (int, error) {
	return 0, nil
}
func (r *toolRepo) ClaimPending(context.Context, string, string, string, string) (ToolCall, error) {
	if r.claimErr != nil {
		return ToolCall{}, r.claimErr
	}
	return r.claimResult, nil
}
func (r *toolRepo) SaveResult(context.Context, string, json.RawMessage) error { return nil }
func (r *toolRepo) ListPendingForSession(context.Context, string, string) ([]ToolCall, error) {
	return nil, nil
}
func (r *toolRepo) GetByToolUseID(context.Context, string, string, string) (ToolCall, error) {
	return ToolCall{}, apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "not found")
}
func (r *toolRepo) ExpirePendingOlderThan(context.Context, time.Time) (int, error) { return 0, nil }

// noopExecutor returns a canned result for every read call. Enough to verify
// that the read-tool branch drives one loop iteration and no proposal fires.
type noopExecutor struct {
	listCalls, getCalls, listProjectsCalls int
}

func (e *noopExecutor) ExecList(context.Context, string, json.RawMessage) (json.RawMessage, error) {
	e.listCalls++
	return json.RawMessage(`{"items":[]}`), nil
}
func (e *noopExecutor) ExecGet(context.Context, string, json.RawMessage) (json.RawMessage, error) {
	e.getCalls++
	return json.RawMessage(`{"id":"t1"}`), nil
}
func (e *noopExecutor) ExecListProjects(context.Context, string) (json.RawMessage, error) {
	e.listProjectsCalls++
	return json.RawMessage(`{"items":[]}`), nil
}
func (e *noopExecutor) ExecComplete(context.Context, string, string, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"id":"t1","status":"done"}`), nil
}
func (e *noopExecutor) ExecReschedule(context.Context, string, string, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"id":"t1"}`), nil
}
func (e *noopExecutor) ExecCreate(context.Context, string, string, json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{"id":"t2"}`), nil
}

// ── tests ─────────────────────────────────────────────────────────────────────

// SendMessage with a read tool: turn 1 emits list_tasks tool_use, executor runs
// inline, turn 2 emits final text — no proposal, no pending row, one tool_result
// user turn was appended between.
func TestSendMessage_ReadToolLoops(t *testing.T) {
	repo := &toolRepo{}
	client := &scriptedClient{streams: []Stream{
		&toolStream{toolUses: []ToolUseBlock{{ID: "tu_1", Name: ToolListTasks, Input: json.RawMessage(`{}`)}}},
		&toolStream{deltas: []string{"Here are your tasks."}},
	}}
	exec := &noopExecutor{}
	svc := NewService(repo, client, "m", nil).WithExecutor(exec).(*service)

	sink := &toolSink{}
	_, err := svc.SendMessage(context.Background(), "u1", "free", "s1", SendMessageRequest{Content: "hi"}, sink)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if exec.listCalls != 1 {
		t.Errorf("list_tasks executor calls = %d, want 1", exec.listCalls)
	}
	if len(sink.proposals) != 0 {
		t.Errorf("read tool must not surface a proposal; got %d", len(sink.proposals))
	}
	if len(repo.pending) != 0 {
		t.Errorf("read tool must not insert a pending row; got %d", len(repo.pending))
	}
	if repo.toolResultsAppnd != 1 {
		t.Errorf("tool_result user turn appended = %d, want 1", repo.toolResultsAppnd)
	}
	if repo.assistantAppends != 2 {
		t.Errorf("assistant turns persisted = %d, want 2", repo.assistantAppends)
	}
	if len(sink.deltas) != 1 || sink.deltas[0] != "Here are your tasks." {
		t.Errorf("final text delta not relayed: %v", sink.deltas)
	}
}

// SendMessage with a write tool: no executor call, one pending row + one
// proposal event, loop stops after the proposal.
func TestSendMessage_WriteToolProposesAndStops(t *testing.T) {
	repo := &toolRepo{}
	client := &scriptedClient{streams: []Stream{
		&toolStream{toolUses: []ToolUseBlock{{
			ID: "tu_1", Name: ToolCompleteTask, Input: json.RawMessage(`{"taskId":"t1"}`),
		}}},
	}}
	exec := &noopExecutor{}
	svc := NewService(repo, client, "m", nil).WithExecutor(exec).(*service)

	sink := &toolSink{}
	_, err := svc.SendMessage(context.Background(), "u1", "free", "s1", SendMessageRequest{Content: "done"}, sink)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	// Executor MUST NOT have run — write tools stay pending.
	if exec.listCalls+exec.getCalls+exec.listProjectsCalls != 0 {
		t.Error("write tool must not execute inline")
	}
	if len(repo.pending) != 1 {
		t.Fatalf("expected 1 pending row, got %d", len(repo.pending))
	}
	if repo.pending[0].ToolName != ToolCompleteTask || repo.pending[0].ToolUseID != "tu_1" {
		t.Errorf("bad pending row: %+v", repo.pending[0])
	}
	if len(sink.proposals) != 1 {
		t.Fatalf("expected 1 tool_proposal event, got %d", len(sink.proposals))
	}
	if sink.proposals[0].ToolUseID != "tu_1" {
		t.Errorf("proposal toolUseId = %q, want tu_1", sink.proposals[0].ToolUseID)
	}
	if repo.toolResultsAppnd != 0 {
		t.Error("proposal must not append a tool_result — that arrives on confirm/reject")
	}
}

// Confirm on a resolved row → 409 CONFLICT surfaces (double-click / race).
func TestConfirmToolCall_AlreadyResolved409(t *testing.T) {
	repo := &toolRepo{
		claimErr: apperror.New(http.StatusConflict, apperror.ErrConflict, "not pending"),
	}
	svc := NewService(repo, &scriptedClient{}, "m", nil).WithExecutor(&noopExecutor{})
	_, err := svc.ConfirmToolCall(context.Background(), "u1", "free", "s1", "tu_1", &toolSink{})
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != http.StatusConflict {
		t.Fatalf("want 409 CONFLICT, got %v", err)
	}
}

// Reject records user_rejected + streams the follow-up. Verifies the tool_result
// user turn is appended and the executor is never called.
func TestRejectToolCall_AppendsRejectionAndStreams(t *testing.T) {
	repo := &toolRepo{
		claimResult: ToolCall{
			ID: "tc1", SessionID: "s1", UserID: "u1", ToolUseID: "tu_1",
			ToolName: ToolCompleteTask, InputJSON: json.RawMessage(`{"taskId":"t1"}`),
		},
	}
	// After reject, the follow-up assistant streams a plain-text acknowledgement.
	client := &scriptedClient{streams: []Stream{
		&toolStream{deltas: []string{"OK, I won't complete it."}},
	}}
	exec := &noopExecutor{}
	svc := NewService(repo, client, "m", nil).WithExecutor(exec)

	sink := &toolSink{}
	_, err := svc.RejectToolCall(context.Background(), "u1", "free", "s1", "tu_1", sink)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if exec.listCalls+exec.getCalls != 0 {
		t.Error("reject must not run any read tool from a stale schedule")
	}
	if repo.toolResultsAppnd != 1 {
		t.Errorf("reject must append one tool_result user turn, got %d", repo.toolResultsAppnd)
	}
	if len(sink.deltas) == 0 {
		t.Error("expected follow-up assistant text delta after reject")
	}
}

// Confirm executor failure ⇒ tool_result carries is_error, and the follow-up
// still streams (so the model can explain the failure).
func TestConfirmToolCall_ExecutorFailureBecomesToolResult(t *testing.T) {
	// Executor rejects with a typed apperror.
	pnf := apperror.New(http.StatusNotFound, apperror.ErrProjectNotFound, "project not found")
	tasks := stubTasks{setStatus: func(context.Context, string, string, string, string) (TaskViewJSON, error) {
		return TaskViewJSON{}, pnf
	}}
	execWithFail := NewToolExecutor(tasks, stubProjects{})

	repo := &toolRepo{
		claimResult: ToolCall{
			ID: "tc1", SessionID: "s1", UserID: "u1", ToolUseID: "tu_1",
			ToolName: ToolCompleteTask, InputJSON: json.RawMessage(`{"taskId":"t1"}`),
		},
	}
	// Follow-up assistant turn: a plain-text explanation.
	client := &scriptedClient{streams: []Stream{
		&toolStream{deltas: []string{"That task isn't in a project I can access."}},
	}}
	svc := NewService(repo, client, "m", nil).WithExecutor(execWithFail)

	sink := &toolSink{}
	if _, err := svc.ConfirmToolCall(context.Background(), "u1", "free", "s1", "tu_1", sink); err != nil {
		t.Fatalf("confirm must not fail on an executor error — it becomes a tool_result: %v", err)
	}
	if repo.toolResultsAppnd != 1 {
		t.Errorf("tool_result user turn expected exactly once, got %d", repo.toolResultsAppnd)
	}
	if len(sink.deltas) == 0 {
		t.Error("expected follow-up assistant text after an executor failure")
	}
}
