package ai

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

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
	// zero-token failure. streamed flips true the moment a delta reaches the sink.
	var messageID string
	err := s.RunWithQuota(ctx, userID, plan, func(ctx context.Context) (bool, error) {
		var streamed bool
		var err error
		messageID, streamed, err = s.runStream(ctx, userID, sessionID, content, sink)
		return streamed, err
	})
	return messageID, err
}

// runStream persists the user turn, streams the completion, and persists the
// assistant turn. streamed is true once any token flushed (keeps the quota
// charge even on a later error or abort); messageID is the persisted assistant id.
func (s *service) runStream(ctx context.Context, userID, sessionID, content string, sink StreamSink) (string, bool, error) {
	// Persist the user message (+ first-turn title) before calling the provider,
	// so an aborted stream still shows the user's turn in GET /sessions/:id.
	if err := s.repo.AppendUserMessage(ctx, sessionID, uuid.New().String(), content, deriveTitle(content)); err != nil {
		return "", false, err
	}

	chatReq, err := s.buildChatRequest(ctx, userID, sessionID)
	if err != nil {
		return "", false, err
	}

	stream, err := s.client.Stream(ctx, chatReq)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = stream.Close() }()

	var assistant strings.Builder
	streamed := false
	for stream.Next() {
		chunk := stream.Text()
		assistant.WriteString(chunk)
		if werr := sink.Delta(chunk); werr != nil {
			// Client gone / write failed mid-stream: stop relaying but persist the
			// partial below. Tokens were generated, so the charge stands.
			break
		}
		streamed = true
	}

	// A provider error surfaces via Err(). If nothing streamed yet, propagate so
	// the handler maps it to an HTTP status and the quota is refunded.
	if serr := stream.Err(); serr != nil {
		if !streamed {
			return "", false, serr
		}
		// Mid-stream provider failure: persist whatever we got, keep the charge,
		// report streamed so the handler emits the terminal error event.
		msgID := s.persistAssistant(ctx, userID, sessionID, assistant.String())
		return msgID, true, serr
	}

	msgID := s.persistAssistant(ctx, userID, sessionID, assistant.String())
	return msgID, streamed || assistant.Len() > 0, nil
}

// persistAssistant writes the (possibly partial) assistant turn and emits the
// WS session-updated event; returns the new message id ("" if nothing to persist
// or the write failed). Best-effort: a persistence or broadcast failure must not
// turn a delivered stream into an error, so it is swallowed, not returned. Uses
// WithoutCancel so an aborted request still records its partial.
func (s *service) persistAssistant(ctx context.Context, userID, sessionID, text string) string {
	if text == "" {
		return ""
	}
	ctx = context.WithoutCancel(ctx)
	msgID := uuid.New().String()
	if err := s.repo.AppendAssistantMessage(ctx, sessionID, msgID, text); err != nil {
		return ""
	}
	s.emitSessionUpdated(userID, sessionID)
	return msgID
}

// buildChatRequest assembles the provider call: system prompt (cached) +
// budgeted history (cache breakpoint on the newest block) + the response cap.
func (s *service) buildChatRequest(ctx context.Context, userID, sessionID string) (ChatRequest, error) {
	pc, err := s.repo.PromptContext(ctx, userID)
	if err != nil {
		return ChatRequest{}, err
	}
	rows, err := s.repo.HistoryFor(ctx, sessionID, historyMaxMessages)
	if err != nil {
		return ChatRequest{}, err
	}
	return ChatRequest{
		Model:       s.model,
		System:      buildSystemPrompt(pc, s.now()),
		CacheSystem: true,
		Messages:    buildHistory(rows),
		MaxTokens:   maxResponseTokens,
	}, nil
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
