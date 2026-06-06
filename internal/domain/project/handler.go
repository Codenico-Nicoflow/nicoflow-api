package project

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Handler handles HTTP requests for the project domain.
type Handler struct{ svc Service }

// NewHandler creates a new project Handler.
func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

// GET /v1/projects
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	f, ok := parseProjectFilter(w, r)
	if !ok {
		return
	}
	resp, err := h.svc.List(r.Context(), userID, f)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, resp)
}

// GET /v1/areas/{areaId}/projects
func (h *Handler) ListByArea(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	areaID := chi.URLParam(r, "areaId")
	f, ok := parseProjectFilter(w, r)
	if !ok {
		return
	}
	resp, err := h.svc.ListByArea(r.Context(), userID, areaID, f)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, resp)
}

// GET /v1/projects/{id}
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

// POST /v1/areas/{areaId}/projects
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	plan := mw.PlanFromCtx(r.Context())
	areaID := chi.URLParam(r, "areaId")

	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	view, err := h.svc.Create(r.Context(), userID, areaID, plan, req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, view)
}

// PATCH /v1/projects/{id}
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	var req UpdateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	view, err := h.svc.Update(r.Context(), userID, id, req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// DELETE /v1/projects/{id}
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.svc.Delete(r.Context(), userID, id); err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.NoContent(w)
}

// PATCH /v1/projects/reorder
func (h *Handler) Reorder(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	var req ReorderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	updated, err := h.svc.Reorder(r.Context(), userID, req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, map[string]int{"updated": updated})
}

// ── helpers ──────────────────────────────────────────────────────────────────

func parseProjectFilter(w http.ResponseWriter, r *http.Request) (ListProjectsFilter, bool) {
	q := r.URL.Query()
	f := ListProjectsFilter{
		Query:  q.Get("q"),
		Cursor: q.Get("cursor"),
	}

	if lStr := q.Get("limit"); lStr != "" {
		l, err := strconv.Atoi(lStr)
		if err != nil || l < 1 || l > 100 {
			respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "limit must be an integer between 1 and 100")
			return ListProjectsFilter{}, false
		}
		f.Limit = l
	}

	if areaID := q.Get("areaId"); areaID != "" {
		f.AreaID = &areaID
	}

	if status := q.Get("status"); status != "" {
		if status != "active" && status != "completed" && status != "archived" {
			respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidStatus, "status must be one of: active, completed, archived")
			return ListProjectsFilter{}, false
		}
		f.Status = &status
	}

	if favStr := q.Get("isFavorite"); favStr != "" {
		if favStr != "true" && favStr != "false" {
			respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "isFavorite must be true or false")
			return ListProjectsFilter{}, false
		}
		fav := favStr == "true"
		f.IsFavorite = &fav
	}

	return f, true
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
