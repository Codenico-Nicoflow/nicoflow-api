package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/cursorutil"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Handler handles HTTP requests for the AI assistant domain.
type Handler struct{ svc Service }

// NewHandler creates a new AI Handler.
func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	respond.Error(w, http.StatusNotImplemented, apperror.ErrInternalServerError, "not implemented")
}

// ListSessions returns the user's sessions, most-recently-updated first.
func (h *Handler) ListSessions(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	views, err := h.svc.ListSessions(r.Context(), userID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, views)
}

// CreateSession creates an empty conversation. Costs no quota.
func (h *Handler) CreateSession(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	var req CreateSessionRequest
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
			return
		}
	}

	view, err := h.svc.CreateSession(r.Context(), userID, req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, view)
}

// GetSession returns a session with its messages (created_at ASC).
func (h *Handler) GetSession(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	view, err := h.svc.GetSession(r.Context(), userID, id)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// DeleteSession removes a session; messages cascade. No quota refund.
func (h *Handler) DeleteSession(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.svc.DeleteSession(r.Context(), userID, id); err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.NoContent(w)
}

// Usage returns the caller's quota state — Free lifetime or Pro month.
func (h *Handler) Usage(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	plan := mw.PlanFromCtx(r.Context())

	view, err := h.svc.Usage(r.Context(), userID, plan)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// ListMessages returns a page of messages for a session, oldest-first (ASC).
// Cursor semantics: nextCursor is non-empty when older messages exist; pass it
// as ?cursor= to load the next (older) page.
func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) {
	cursor, limit, err := cursorutil.ParsePageParams(r)
	if err != nil {
		respond.Error(w, http.StatusUnprocessableEntity, apperror.ErrInvalidInput, err.Error())
		return
	}
	result, err := h.svc.ListSessionMessages(
		r.Context(),
		mw.UserIDFromCtx(r.Context()),
		chi.URLParam(r, "id"),
		ListMessagesFilter{Cursor: cursor, Limit: limit},
	)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, result)
}
func (h *Handler) ParseNLP(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }

// ListToolCalls returns the session's pending write-tool proposals. Read;
// includes only status=pending — resolved rows are audit data.
func (h *Handler) ListToolCalls(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	sessionID := chi.URLParam(r, "id")

	views, err := h.svc.ListPendingToolCalls(r.Context(), userID, sessionID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, views)
}

// ConfirmToolCall streams the follow-up assistant response after executing a
// pending proposal. Same SSE shape as SendMessage.
func (h *Handler) ConfirmToolCall(w http.ResponseWriter, r *http.Request) {
	h.resolveToolCall(w, r, true)
}

// RejectToolCall streams the follow-up assistant response after marking the
// proposal rejected. Same SSE shape as SendMessage.
func (h *Handler) RejectToolCall(w http.ResponseWriter, r *http.Request) {
	h.resolveToolCall(w, r, false)
}

// resolveToolCall is the shared body of Confirm/Reject: run the service action,
// stream the follow-up over SSE. Pre-stream errors return a normal envelope;
// once tokens flow (or a new tool_proposal fires), terminal outcomes ride the
// event stream. No additional quota is reserved — the original SendMessage
// already paid for this whole user-turn.
func (h *Handler) resolveToolCall(w http.ResponseWriter, r *http.Request, confirm bool) {
	userID := mw.UserIDFromCtx(r.Context())
	plan := mw.PlanFromCtx(r.Context())
	sessionID := chi.URLParam(r, "id")
	toolUseID := chi.URLParam(r, "toolUseId")

	flusher, ok := w.(http.Flusher)
	if !ok {
		respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "streaming unsupported")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), wholeStreamCeiling)
	defer cancel()

	sink := &sseSink{w: w, flusher: flusher}
	var messageID string
	var err error
	if confirm {
		messageID, err = h.svc.ConfirmToolCall(ctx, userID, plan, sessionID, toolUseID, sink)
	} else {
		messageID, err = h.svc.RejectToolCall(ctx, userID, plan, sessionID, toolUseID, sink)
	}

	if !sink.committed {
		if err != nil {
			writeAppError(w, r, err)
			return
		}
		sink.commit()
		sink.writeDone(messageID, h.usage(r.Context(), userID, plan))
		return
	}
	if err != nil {
		log.Warn().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("ai tool-call resolve error after commit")
		sink.writeError(StreamErrorCode(err))
		return
	}
	sink.writeDone(messageID, h.usage(r.Context(), userID, plan))
}

// wholeStreamCeiling caps the entire completion; a runaway stream is cut here.
const wholeStreamCeiling = 90 * time.Second

// SendMessage streams a Claude response as SSE over the POST body. Pre-stream
// failures return a normal JSON error envelope with the right status; once the
// first token flushes the status is committed and terminal outcomes (done/error)
// ride the event stream instead.
func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	plan := mw.PlanFromCtx(r.Context())
	sessionID := chi.URLParam(r, "id")

	var req SendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "streaming unsupported")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), wholeStreamCeiling)
	defer cancel()

	sink := &sseSink{w: w, flusher: flusher}
	messageID, err := h.svc.SendMessage(ctx, userID, plan, sessionID, req, sink)

	if !sink.committed {
		// Nothing streamed: the HTTP status is still ours to set.
		if err != nil {
			writeAppError(w, r, err)
			return
		}
		// A no-token success is unexpected but must still terminate the stream.
		sink.commit()
		sink.writeDone(messageID, h.usage(r.Context(), userID, plan))
		return
	}

	// Stream already committed: terminate over SSE, never a second HTTP status.
	if err != nil {
		log.Warn().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("ai stream error after commit")
		sink.writeError(StreamErrorCode(err))
		return
	}
	sink.writeDone(messageID, h.usage(r.Context(), userID, plan))
}

// usage reads fresh quota state for the done event; a read failure yields a zero
// UsageView rather than failing an already-delivered stream.
func (h *Handler) usage(ctx context.Context, userID, plan string) UsageView {
	view, err := h.svc.Usage(ctx, userID, plan)
	if err != nil {
		return UsageView{}
	}
	return view
}

// sseSink is the handler-side StreamSink: it writes text/event-stream frames and
// flushes each one. The first Delta commits the SSE headers + 200 status.
type sseSink struct {
	w         http.ResponseWriter
	flusher   http.Flusher
	committed bool
}

func (s *sseSink) Delta(text string) error {
	if !s.committed {
		s.commit()
	}
	if err := s.writeEvent(deltaEvent{Type: "delta", Text: text}); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// ToolProposal writes one tool_proposal SSE frame. Committing (like Delta)
// transitions the response to a streaming body — the sink is the only place
// the HTTP status becomes 200 for this endpoint.
func (s *sseSink) ToolProposal(assistantMessageID, toolUseID, toolName string, input json.RawMessage) error {
	if !s.committed {
		s.commit()
	}
	ev := toolProposalEvent{
		Type: "tool_proposal", AssistantMessageID: assistantMessageID,
		ToolUseID: toolUseID, ToolName: toolName, Input: input,
	}
	if err := s.writeEvent(ev); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

func (s *sseSink) commit() {
	h := s.w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	s.w.WriteHeader(http.StatusOK)
	s.committed = true
}

func (s *sseSink) writeDone(messageID string, usage UsageView) {
	_ = s.writeEvent(doneEvent{Type: "done", MessageID: messageID, Usage: usage})
	s.flusher.Flush()
}

func (s *sseSink) writeError(code string) {
	_ = s.writeEvent(errorEvent{Type: "error", Code: code})
	s.flusher.Flush()
}

// writeEvent marshals one SSE `data:` frame. Content is never logged.
func (s *sseSink) writeEvent(payload any) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(s.w, "data: %s\n\n", b)
	return err
}

func writeAppError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apperror.AppError
	if errors.As(err, &ae) {
		respond.Error(w, ae.Status, ae.Code, ae.Message)
		return
	}
	log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("unexpected internal error")
	respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "an unexpected error occurred")
}
