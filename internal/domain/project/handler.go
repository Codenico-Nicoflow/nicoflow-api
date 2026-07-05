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

// List godoc
// @Summary      List projects
// @Description  Lists the user's projects across all areas, cursor-paginated, with optional filters.
// @Tags         projects
// @Produce      json
// @Param        q           query     string  false  "Search by project name"
// @Param        areaId      query     string  false  "Filter by area"
// @Param        status      query     string  false  "Filter by status (active|completed|archived)"
// @Param        isFavorite  query     bool    false  "Filter by favorite"
// @Param        limit       query     int     false  "Page size (1–100)"
// @Param        cursor      query     string  false  "Pagination cursor"
// @Security     BearerAuth
// @Success      200  {object}  ProjectListEnvelope  "Paginated projects"
// @Failure      400  {object}  ErrorEnvelope        "INVALID_INPUT"
// @Router       /projects [get]
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

// ListByArea godoc
// @Summary      List projects in an area
// @Description  Lists the projects within one area, cursor-paginated, with optional filters.
// @Tags         projects
// @Produce      json
// @Param        areaId      path      string  true   "Area ID"
// @Param        q           query     string  false  "Search by project name"
// @Param        status      query     string  false  "Filter by status (active|completed|archived)"
// @Param        isFavorite  query     bool    false  "Filter by favorite"
// @Param        limit       query     int     false  "Page size (1–100)"
// @Param        cursor      query     string  false  "Pagination cursor"
// @Security     BearerAuth
// @Success      200  {object}  ProjectListEnvelope  "Paginated projects"
// @Failure      400  {object}  ErrorEnvelope        "INVALID_INPUT"
// @Router       /areas/{areaId}/projects [get]
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

// Get godoc
// @Summary      Get a project
// @Description  Retrieves a single project by ID. Cross-user access returns 404.
// @Tags         projects
// @Produce      json
// @Param        id   path      string  true  "Project ID"
// @Security     BearerAuth
// @Success      200  {object}  ProjectEnvelope  "The project"
// @Failure      404  {object}  ErrorEnvelope    "PROJECT_NOT_FOUND"
// @Router       /projects/{id} [get]
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

// Create godoc
// @Summary      Create a project
// @Description  Creates a project inside an area. Free plan allows up to 5 projects total.
// @Tags         projects
// @Accept       json
// @Produce      json
// @Param        areaId  path      string                true  "Area ID"
// @Param        body    body      CreateProjectRequest  true  "Project to create"
// @Security     BearerAuth
// @Success      201  {object}  ProjectEnvelope  "The created project"
// @Failure      403  {object}  ErrorEnvelope    "PLAN_LIMIT_EXCEEDED"
// @Failure      404  {object}  ErrorEnvelope    "AREA_NOT_FOUND"
// @Failure      409  {object}  ErrorEnvelope    "DUPLICATE_NAME"
// @Failure      422  {object}  ErrorEnvelope    "INVALID_INPUT"
// @Router       /areas/{areaId}/projects [post]
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

// Update godoc
// @Summary      Update a project
// @Description  Partial update of a project. Setting areaId moves the project between areas (null detaches).
// @Tags         projects
// @Accept       json
// @Produce      json
// @Param        id    path      string                true  "Project ID"
// @Param        body  body      UpdateProjectRequest  true  "Fields to update (all optional)"
// @Security     BearerAuth
// @Success      200  {object}  ProjectEnvelope  "The updated project"
// @Failure      404  {object}  ErrorEnvelope    "PROJECT_NOT_FOUND"
// @Failure      409  {object}  ErrorEnvelope    "DUPLICATE_NAME"
// @Failure      422  {object}  ErrorEnvelope    "INVALID_INPUT"
// @Router       /projects/{id} [patch]
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

// Delete godoc
// @Summary      Delete a project
// @Description  Deletes a project; its tasks are cascade-deleted.
// @Tags         projects
// @Param        id   path  string  true  "Project ID"
// @Security     BearerAuth
// @Success      204  "No Content"
// @Failure      404  {object}  ErrorEnvelope  "PROJECT_NOT_FOUND"
// @Router       /projects/{id} [delete]
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.svc.Delete(r.Context(), userID, id); err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.NoContent(w)
}

// Reorder godoc
// @Summary      Reorder projects
// @Description  Bulk-updates project displayOrder in one transaction (atomic; a foreign id rolls the whole batch back).
// @Tags         projects
// @Accept       json
// @Produce      json
// @Param        body  body      ReorderRequest  true  "New order (id → displayOrder)"
// @Security     BearerAuth
// @Success      200  {object}  ReorderResultEnvelope  "Number of rows updated"
// @Failure      404  {object}  ErrorEnvelope          "PROJECT_NOT_FOUND (a foreign id — whole batch rolled back)"
// @Failure      422  {object}  ErrorEnvelope          "INVALID_INPUT"
// @Router       /projects/reorder [patch]
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
