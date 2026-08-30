package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// maxAssistantTurns bounds the internal tool loop per one user message —
// includes any read-tool round-trips. A runaway assistant is stopped here.
const maxAssistantTurns = 4

// tryAcquire marks a session as streaming. false ⇒ a stream is already active.
func (s *service) tryAcquire(sessionID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, busy := s.active[sessionID]; busy {
		return false
	}
	s.active[sessionID] = struct{}{}
	return true
}

func (s *service) release(sessionID string) {
	s.mu.Lock()
	delete(s.active, sessionID)
	s.mu.Unlock()
}

// SendMessage implements the send pipeline. Order is load-bearing: ownership →
// single-stream guard → quota reserve → persist user turn → stream → persist
// assistant turn → WS emit. See the interface doc for the full contract.
func (s *service) SendMessage(ctx context.Context, userID, plan, sessionID string, req SendMessageRequest, sink StreamSink) (string, error) {
	content := strings.TrimSpace(req.Content)
	if n := len(content); n < 1 || n > maxContentLen {
		return "", apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "content must be 1..2000 characters")
	}

	// Ownership (also 404s a foreign/missing session) before touching anything.
	if _, err := s.repo.GetSession(ctx, userID, sessionID); err != nil {
		return "", err
	}

	// Single-stream guard — one in-flight completion per session.
	if !s.tryAcquire(sessionID) {
		return "", apperror.New(http.StatusConflict, apperror.ErrAIStreamActive, "a response is already streaming for this session")
	}
	defer s.release(sessionID)

	// Quota reserve wraps the metered work; the reservation is refunded only on a
	// zero-token failure. streamed flips true the moment a delta reaches the sink
	// OR a tool_proposal event is emitted (both count as work produced).
	var messageID string
	err := s.RunWithQuota(ctx, userID, plan, func(ctx context.Context) (bool, error) {
		var streamed bool
		var err error
		messageID, streamed, err = s.runSendMessage(ctx, userID, plan, sessionID, content, sink)
		return streamed, err
	})
	return messageID, err
}

// runSendMessage persists the user turn then drives the tool loop.
func (s *service) runSendMessage(ctx context.Context, userID, plan, sessionID, content string, sink StreamSink) (string, bool, error) {
	if err := s.repo.AppendUserMessage(ctx, sessionID, uuid.New().String(), content, deriveTitle(content)); err != nil {
		return "", false, err
	}
	return s.runToolLoop(ctx, userID, plan, sessionID, sink)
}

// loopState carries mutable state across iterations of runToolLoop. Split out
// so each turn helper stays small and the loop itself stays readable.
type loopState struct {
	lastAssistantID string
	rootAssistantID string
	streamed        bool
}

// runToolLoop is the bounded assistant/tool-result cycle. Up to maxAssistantTurns
// assistant turns; each turn's tool_use blocks either execute inline (read) or
// become a persisted proposal (write) that stops the loop. Returns the id of
// the assistant message that produced the terminal event (the last written).
func (s *service) runToolLoop(ctx context.Context, userID, plan, sessionID string, sink StreamSink) (string, bool, error) {
	st := loopState{}
	for turn := 0; turn < maxAssistantTurns; turn++ {
		done, err := s.runOneTurn(ctx, userID, plan, sessionID, sink, &st)
		if err != nil || done {
			return st.lastAssistantID, st.streamed, err
		}
	}
	return st.lastAssistantID, st.streamed, nil
}

// runOneTurn is one assistant round-trip inside runToolLoop. Returns done=true
// when the loop must stop (no more tool_uses, sink failed, provider error,
// write-proposal emitted, or the anti-abuse ceiling tripped).
func (s *service) runOneTurn(ctx context.Context, userID, plan, sessionID string, sink StreamSink, st *loopState) (bool, error) {
	chatReq, err := s.buildChatRequest(ctx, userID, sessionID)
	if err != nil {
		return true, err
	}
	provStream, err := s.client.Stream(ctx, chatReq)
	if err != nil {
		return true, err
	}
	defer func() { _ = provStream.Close() }()

	text, sinkFailed, streamErr := drainStream(provStream, sink)
	toolUses := provStream.ToolUses()
	s.recordAssistantTurn(ctx, userID, sessionID, text, toolUses, sinkFailed, st)

	if text != "" || len(toolUses) > 0 {
		st.streamed = true
	}
	if sinkFailed {
		return true, nil
	}
	if streamErr != nil {
		return true, streamErr
	}
	if len(toolUses) == 0 {
		return true, nil
	}
	if s.exceedsToolCallCap(ctx, sessionID, st.rootAssistantID, len(toolUses)) {
		return true, nil
	}

	results, wroteProposal, wErr := s.handleToolUses(ctx, userID, plan, sessionID, st.lastAssistantID, toolUses, sink)
	if wErr != nil {
		return true, wErr
	}
	if wroteProposal {
		st.streamed = true
		return true, nil
	}
	if len(results) == 0 {
		return true, nil
	}
	if err := s.appendToolResults(ctx, sessionID, results); err != nil {
		return true, err
	}
	return false, nil
}

// recordAssistantTurn persists the assistant turn and updates loop state.
func (s *service) recordAssistantTurn(ctx context.Context, userID, sessionID, text string, toolUses []ToolUseBlock, sinkFailed bool, st *loopState) {
	if text == "" && len(toolUses) == 0 && !sinkFailed {
		return
	}
	id := s.persistAssistantWithTools(ctx, userID, sessionID, text, toolUses)
	if id == "" {
		return
	}
	st.lastAssistantID = id
	if st.rootAssistantID == "" && len(toolUses) > 0 {
		st.rootAssistantID = id
	}
}

// exceedsToolCallCap returns true when adding `wantMore` new tool_call rows to
// the current root turn would exceed MaxToolCallsPerTurn. A repo error here is
// swallowed (best-effort ceiling) — the loop can still hit maxAssistantTurns.
func (s *service) exceedsToolCallCap(ctx context.Context, sessionID, rootAssistantID string, wantMore int) bool {
	if rootAssistantID == "" || s.executor == nil {
		return false
	}
	existing, err := s.repo.CountForAssistantMessage(ctx, sessionID, rootAssistantID)
	if err != nil {
		return false
	}
	return existing+wantMore > MaxToolCallsPerTurn
}

// drainStream pulls every delta from provStream to sink until the stream
// finishes or the sink write fails. Returns the concatenated text, a
// sink-failed flag (client dropped), and the terminal stream error.
func drainStream(provStream Stream, sink StreamSink) (string, bool, error) {
	var sb strings.Builder
	sinkFailed := false
	for provStream.Next() {
		chunk := provStream.Text()
		sb.WriteString(chunk)
		if werr := sink.Delta(chunk); werr != nil {
			sinkFailed = true
			break
		}
	}
	return sb.String(), sinkFailed, provStream.Err()
}

// handleToolUses splits tool_uses into read-then-execute and write-then-propose.
// wroteProposal is true if any write proposal was emitted; the caller stops
// looping in that case.
func (s *service) handleToolUses(
	ctx context.Context, userID, plan, sessionID, assistantMessageID string,
	toolUses []ToolUseBlock, sink StreamSink,
) ([]ToolResultBlock, bool, error) {
	if s.executor == nil {
		// Tools not wired: return a synthetic error result for each so the
		// model doesn't retry forever expecting output.
		results := make([]ToolResultBlock, len(toolUses))
		for i, tu := range toolUses {
			results[i] = ToolResultBlock{
				ToolUseID: tu.ID,
				Content:   `{"code":"AI_UNAVAILABLE","message":"tools are not enabled"}`,
				IsError:   true,
			}
		}
		return results, false, nil
	}

	wroteProposal := false
	var results []ToolResultBlock
	for _, tu := range toolUses {
		if IsWriteTool(tu.Name) {
			if err := s.emitWriteProposal(ctx, userID, sessionID, assistantMessageID, tu, sink); err != nil {
				return nil, wroteProposal, err
			}
			wroteProposal = true
			continue
		}
		// Read tool: execute inline, produce a tool_result.
		result, execErr := s.execReadTool(ctx, userID, tu)
		if execErr != nil {
			results = append(results, ToolResultBlock{
				ToolUseID: tu.ID,
				Content:   EncodeExecErr(execErr),
				IsError:   true,
			})
			continue
		}
		results = append(results, ToolResultBlock{
			ToolUseID: tu.ID,
			Content:   string(result),
			IsError:   false,
		})
	}
	return results, wroteProposal, nil
}

// execReadTool routes a read tool by name.
func (s *service) execReadTool(ctx context.Context, userID string, tu ToolUseBlock) (json.RawMessage, error) {
	switch tu.Name {
	case ToolListTasks:
		return s.executor.ExecList(ctx, userID, tu.Input)
	case ToolGetTask:
		return s.executor.ExecGet(ctx, userID, tu.Input)
	case ToolListProjects:
		return s.executor.ExecListProjects(ctx, userID)
	default:
		return nil, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "unknown tool: "+tu.Name)
	}
}

// emitWriteProposal persists the pending row and emits the tool_proposal event.
// assistantMessageID may be empty if the persist failed — the proposal cannot
// be recorded then, so we bail with an error rather than emit a proposal the
// user can't confirm.
func (s *service) emitWriteProposal(
	ctx context.Context, userID, sessionID, assistantMessageID string,
	tu ToolUseBlock, sink StreamSink,
) error {
	if assistantMessageID == "" {
		return apperror.New(http.StatusInternalServerError, apperror.ErrInternalServerError, "cannot record tool proposal: assistant message not persisted")
	}
	err := s.repo.InsertPending(ctx, ToolCall{
		ID:                 uuid.New().String(),
		SessionID:          sessionID,
		UserID:             userID,
		AssistantMessageID: assistantMessageID,
		ToolUseID:          tu.ID,
		ToolName:           tu.Name,
		InputJSON:          tu.Input,
	})
	if err != nil {
		return err
	}
	return sink.ToolProposal(assistantMessageID, tu.ID, tu.Name, tu.Input)
}

// persistAssistantWithTools writes an assistant turn (text + tool_uses) as one
// row: `content` gets the concatenated visible text (backward compat) and
// `content_json` gets the full block sequence for a lossless replay.
func (s *service) persistAssistantWithTools(ctx context.Context, userID, sessionID, text string, toolUses []ToolUseBlock) string {
	if text == "" && len(toolUses) == 0 {
		return ""
	}
	ctx = context.WithoutCancel(ctx)
	msgID := uuid.New().String()

	blocks := buildContentJSON(text, toolUses)
	if err := s.repo.AppendAssistantMessageWithBlocks(ctx, sessionID, msgID, text, blocks); err != nil {
		return ""
	}
	s.emitSessionUpdated(userID, sessionID)
	return msgID
}

// appendToolResults writes a user turn that carries only tool_result blocks —
// the pattern Claude expects between an assistant tool_use and the next
// assistant turn. text stays empty; the DB row uses role='user' with
// content=” and content_json holding the tool_result blocks.
func (s *service) appendToolResults(ctx context.Context, sessionID string, results []ToolResultBlock) error {
	blocks := buildToolResultsJSON(results)
	return s.repo.AppendToolResultsMessage(ctx, sessionID, uuid.New().String(), blocks)
}

// buildContentJSON builds the assistant turn's content_json — an ordered array
// of blocks (text first if non-empty, then each tool_use).
func buildContentJSON(text string, toolUses []ToolUseBlock) json.RawMessage {
	type textBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type toolUseBlockJSON struct {
		Type  string          `json:"type"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	blocks := make([]any, 0, 1+len(toolUses))
	if text != "" {
		blocks = append(blocks, textBlock{Type: "text", Text: text})
	}
	for _, tu := range toolUses {
		blocks = append(blocks, toolUseBlockJSON{
			Type: "tool_use", ID: tu.ID, Name: tu.Name, Input: tu.Input,
		})
	}
	raw, _ := json.Marshal(blocks)
	return raw
}

// buildToolResultsJSON packages tool_result blocks into content_json for the
// user turn that carries them.
func buildToolResultsJSON(results []ToolResultBlock) json.RawMessage {
	type toolResultBlockJSON struct {
		Type      string `json:"type"`
		ToolUseID string `json:"tool_use_id"`
		Content   string `json:"content"`
		IsError   bool   `json:"is_error,omitempty"`
	}
	blocks := make([]toolResultBlockJSON, len(results))
	for i, r := range results {
		blocks[i] = toolResultBlockJSON{
			Type: "tool_result", ToolUseID: r.ToolUseID,
			Content: r.Content, IsError: r.IsError,
		}
	}
	raw, _ := json.Marshal(blocks)
	return raw
}

// buildChatRequest assembles the provider call: system prompt (cached) +
// budgeted history (cache breakpoint on the newest block) + the response cap +
// tools (when the executor is wired).
func (s *service) buildChatRequest(ctx context.Context, userID, sessionID string) (ChatRequest, error) {
	pc, err := s.repo.PromptContext(ctx, userID)
	if err != nil {
		return ChatRequest{}, err
	}
	rows, err := s.repo.HistoryForWithBlocks(ctx, sessionID, historyMaxMessages)
	if err != nil {
		return ChatRequest{}, err
	}
	req := ChatRequest{
		Model:       s.model,
		System:      buildSystemPrompt(pc, s.now()),
		CacheSystem: true,
		Messages:    buildHistoryWithBlocks(rows),
		MaxTokens:   maxResponseTokens,
	}
	if s.executor != nil {
		req.Tools = AvailableTools(DefaultTools(), s.executor.AvailableTools())
	}
	return req, nil
}

// emitSessionUpdated fans out ai.session.updated (fire-and-forget).
func (s *service) emitSessionUpdated(userID, sessionID string) {
	if s.broadcaster == nil {
		return
	}
	s.broadcaster.Broadcast(userID, Event{
		Type:    EventSessionUpdated,
		Payload: map[string]string{"id": sessionID},
	})
}

// StreamErrorCode maps a pipeline error to its terminal SSE error code. Only the
// provider error family reaches a mid-stream terminal event; everything else was
// already returned before the status committed.
func StreamErrorCode(err error) string {
	var ae *apperror.AppError
	if errors.As(err, &ae) {
		return ae.Code
	}
	return apperror.ErrAIProviderError
}

// ConfirmToolCall runs the confirm pipeline: claim → execute → save → re-stream.
func (s *service) ConfirmToolCall(ctx context.Context, userID, plan, sessionID, toolUseID string, sink StreamSink) (string, error) {
	// Ownership: implicit — Claim's WHERE filters on user_id.
	if !s.tryAcquire(sessionID) {
		return "", apperror.New(http.StatusConflict, apperror.ErrAIStreamActive, "a response is already streaming for this session")
	}
	defer s.release(sessionID)

	tc, err := s.repo.ClaimPending(ctx, sessionID, toolUseID, userID, ToolCallStatusConfirmed)
	if err != nil {
		return "", err
	}

	// Execute (executor errors → tool_result with is_error, NOT a handler error).
	var result ToolResultBlock
	if s.executor == nil {
		result = ToolResultBlock{
			ToolUseID: tc.ToolUseID,
			Content:   `{"code":"AI_UNAVAILABLE","message":"tools are not enabled"}`,
			IsError:   true,
		}
	} else {
		payload, execErr := s.execWriteTool(ctx, userID, plan, tc)
		if execErr != nil {
			result = ToolResultBlock{
				ToolUseID: tc.ToolUseID,
				Content:   EncodeExecErr(execErr),
				IsError:   true,
			}
		} else {
			result = ToolResultBlock{
				ToolUseID: tc.ToolUseID,
				Content:   string(payload),
				IsError:   false,
			}
		}
	}
	// Store the result on the row (best-effort — a save failure doesn't block
	// the follow-up stream; the model still sees the tool_result via history).
	_ = s.repo.SaveResult(context.WithoutCancel(ctx), tc.ID, json.RawMessage(result.Content))

	// Append tool_result as a user turn, then re-stream the follow-up.
	if err := s.appendToolResults(ctx, sessionID, []ToolResultBlock{result}); err != nil {
		return "", err
	}
	messageID, _, streamErr := s.runToolLoop(ctx, userID, plan, sessionID, sink)
	return messageID, streamErr
}

// RejectToolCall claims + inserts a user_rejected tool_result + re-streams.
func (s *service) RejectToolCall(ctx context.Context, userID, plan, sessionID, toolUseID string, sink StreamSink) (string, error) {
	if !s.tryAcquire(sessionID) {
		return "", apperror.New(http.StatusConflict, apperror.ErrAIStreamActive, "a response is already streaming for this session")
	}
	defer s.release(sessionID)

	tc, err := s.repo.ClaimPending(ctx, sessionID, toolUseID, userID, ToolCallStatusRejected)
	if err != nil {
		return "", err
	}
	result := ToolResultBlock{
		ToolUseID: tc.ToolUseID,
		Content:   "user_rejected",
		IsError:   false,
	}
	_ = s.repo.SaveResult(context.WithoutCancel(ctx), tc.ID, json.RawMessage(`{"outcome":"user_rejected"}`))
	if err := s.appendToolResults(ctx, sessionID, []ToolResultBlock{result}); err != nil {
		return "", err
	}
	messageID, _, streamErr := s.runToolLoop(ctx, userID, plan, sessionID, sink)
	return messageID, streamErr
}

// execWriteTool routes a write tool through the executor.
func (s *service) execWriteTool(ctx context.Context, userID, plan string, tc ToolCall) (json.RawMessage, error) {
	switch tc.ToolName {
	case ToolCompleteTask:
		return s.executor.ExecComplete(ctx, userID, plan, tc.InputJSON)
	case ToolRescheduleTask:
		return s.executor.ExecReschedule(ctx, userID, plan, tc.InputJSON)
	case ToolCreateTask:
		return s.executor.ExecCreate(ctx, userID, plan, tc.InputJSON)
	case ToolSetupRecurringTask:
		return s.executor.ExecSetupRecurring(ctx, userID, plan, tc.InputJSON)
	case ToolAdjustRecurringTask:
		return s.executor.ExecAdjustRecurring(ctx, userID, plan, tc.InputJSON)
	case ToolPauseRecurringTask:
		return s.executor.ExecPauseRecurring(ctx, userID, plan, tc.InputJSON)
	case ToolEndRecurringSeries:
		return s.executor.ExecEndRecurringSeries(ctx, userID, tc.InputJSON)
	case ToolCreateNote:
		return s.executor.ExecCreateNote(ctx, userID, tc.InputJSON)
	case ToolCreateArea:
		return s.executor.ExecCreateArea(ctx, userID, plan, tc.InputJSON)
	case ToolCreateProject:
		return s.executor.ExecCreateProject(ctx, userID, plan, tc.InputJSON)
	case ToolUpdateProject:
		return s.executor.ExecUpdateProject(ctx, userID, tc.InputJSON)
	case ToolAddSubtask:
		return s.executor.ExecAddSubtask(ctx, userID, tc.InputJSON)
	case ToolCompleteSubtask:
		return s.executor.ExecCompleteSubtask(ctx, userID, tc.InputJSON)
	case ToolProcessBucketItem:
		return s.executor.ExecProcessBucketItem(ctx, userID, plan, tc.InputJSON)
	default:
		return nil, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "unknown write tool: "+tc.ToolName)
	}
}

// ListPendingToolCalls returns the session's pending proposals.
func (s *service) ListPendingToolCalls(ctx context.Context, userID, sessionID string) ([]ToolCallView, error) {
	if _, err := s.repo.GetSession(ctx, userID, sessionID); err != nil {
		return nil, err
	}
	rows, err := s.repo.ListPendingForSession(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	out := make([]ToolCallView, len(rows))
	for i, r := range rows {
		out[i] = toolCallToView(r)
	}
	return out, nil
}
