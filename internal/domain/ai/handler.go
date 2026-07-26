package ai

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
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

func (h *Handler) ListMessages(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func (h *Handler) SendMessage(w http.ResponseWriter, r *http.Request)  { notImplemented(w, r) }
func (h *Handler) ParseNLP(w http.ResponseWriter, r *http.Request)     { notImplemented(w, r) }

func writeAppError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apperror.AppError
	if errors.As(err, &ae) {
		respond.Error(w, ae.Status, ae.Code, ae.Message)
		return
	}
	log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("unexpected internal error")
	respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "an unexpected error occurred")
}
