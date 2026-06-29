package task

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

// Handler handles HTTP requests for the task domain.
type Handler struct{ svc Service }

// NewHandler creates a new task Handler.
func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

// GET /v1/projects/{projectId}/tasks
func (h *Handler) ListByProject(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	projectID := chi.URLParam(r, "projectId")

	resp, err := h.svc.ListByProject(r.Context(), userID, projectID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, resp)
}

// GET /v1/tasks/{id}
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	view, err := h.svc.Get(r.Context(), userID, id)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// POST /v1/projects/{projectId}/tasks
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	plan := mw.PlanFromCtx(r.Context())
	projectID := chi.URLParam(r, "projectId")

	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	view, err := h.svc.Create(r.Context(), userID, projectID, plan, req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, view)
}

// PATCH /v1/tasks/{id}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	plan := mw.PlanFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	var req UpdateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	view, err := h.svc.Update(r.Context(), userID, id, plan, req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// DELETE /v1/tasks/{id}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.svc.Delete(r.Context(), userID, id); err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.NoContent(w)
}

// ── not yet implemented (later E-013 stories) ────────────────────────────────

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	respond.Error(w, http.StatusNotImplemented, apperror.ErrInternalServerError, "not implemented")
}

func (h *Handler) ListSubtasks(w http.ResponseWriter, r *http.Request)       { notImplemented(w, r) }
func (h *Handler) CreateSubtask(w http.ResponseWriter, r *http.Request)      { notImplemented(w, r) }
func (h *Handler) UpdateSubtask(w http.ResponseWriter, r *http.Request)      { notImplemented(w, r) }
func (h *Handler) DeleteSubtask(w http.ResponseWriter, r *http.Request)      { notImplemented(w, r) }
func (h *Handler) TimeSpread(w http.ResponseWriter, r *http.Request)         { notImplemented(w, r) }
func (h *Handler) Search(w http.ResponseWriter, r *http.Request)             { notImplemented(w, r) }
func (h *Handler) ListAttachments(w http.ResponseWriter, r *http.Request)    { notImplemented(w, r) }
func (h *Handler) CreateAttachment(w http.ResponseWriter, r *http.Request)   { notImplemented(w, r) }
func (h *Handler) DownloadAttachment(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func (h *Handler) DeleteAttachment(w http.ResponseWriter, r *http.Request)   { notImplemented(w, r) }

// ── helpers ──────────────────────────────────────────────────────────────────

func writeAppError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apperror.AppError
	if errors.As(err, &ae) {
		respond.Error(w, ae.Status, ae.Code, ae.Message)
		return
	}
	log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("unexpected internal error")
	respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "an unexpected error occurred")
}
