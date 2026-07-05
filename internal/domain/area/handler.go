package area

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

// Handler handles HTTP requests for the area domain.
type Handler struct{ svc Service }

// NewHandler creates a new area Handler.
func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

// List godoc
// @Summary      List areas
// @Description  Lists the user's areas, cursor-paginated, with optional name search.
// @Tags         areas
// @Produce      json
// @Param        q       query     string  false  "Search by area name"
// @Param        limit   query     int     false  "Page size (1–100)"
// @Param        cursor  query     string  false  "Pagination cursor"
// @Security     BearerAuth
// @Success      200  {object}  AreaListEnvelope  "Paginated areas"
// @Failure      400  {object}  ErrorEnvelope     "INVALID_INPUT (bad limit/cursor)"
// @Router       /areas [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	f := ListAreasFilter{
		Query:  r.URL.Query().Get("q"),
		Cursor: r.URL.Query().Get("cursor"),
	}
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		l, err := strconv.Atoi(lStr)
		if err != nil || l < 1 || l > 100 {
			respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "limit must be an integer between 1 and 100")
			return
		}
		f.Limit = l
	}

	resp, err := h.svc.List(r.Context(), userID, f)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, resp)
}

// ListWithProjects godoc
// @Summary      List areas with nested projects
// @Description  Returns every area with its projects nested — the payload the board renders from.
// @Tags         areas
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  AreaWithProjectsEnvelope  "Areas with nested projects"
// @Router       /areas/with-projects [get]
func (h *Handler) ListWithProjects(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	views, err := h.svc.ListWithProjects(r.Context(), userID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, views)
}

// Get godoc
// @Summary      Get an area
// @Description  Retrieves a single area by ID. Cross-user access returns 404.
// @Tags         areas
// @Produce      json
// @Param        id   path      string  true  "Area ID"
// @Security     BearerAuth
// @Success      200  {object}  AreaEnvelope   "The area"
// @Failure      404  {object}  ErrorEnvelope  "AREA_NOT_FOUND"
// @Router       /areas/{id} [get]
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
// @Summary      Create an area
// @Description  Creates an area. Free plan allows up to 3 areas.
// @Tags         areas
// @Accept       json
// @Produce      json
// @Param        body  body      CreateAreaRequest  true  "Area to create"
// @Security     BearerAuth
// @Success      201  {object}  AreaEnvelope   "The created area"
// @Failure      403  {object}  ErrorEnvelope  "PLAN_LIMIT_EXCEEDED"
// @Failure      409  {object}  ErrorEnvelope  "DUPLICATE_NAME"
// @Failure      422  {object}  ErrorEnvelope  "INVALID_INPUT"
// @Router       /areas [post]
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	plan := mw.PlanFromCtx(r.Context())

	var req CreateAreaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	view, err := h.svc.Create(r.Context(), userID, plan, req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, view)
}

// Update godoc
// @Summary      Update an area
// @Description  Partial update of an area (name, color, icon).
// @Tags         areas
// @Accept       json
// @Produce      json
// @Param        id    path      string             true  "Area ID"
// @Param        body  body      UpdateAreaRequest  true  "Fields to update (all optional)"
// @Security     BearerAuth
// @Success      200  {object}  AreaEnvelope   "The updated area"
// @Failure      404  {object}  ErrorEnvelope  "AREA_NOT_FOUND"
// @Failure      409  {object}  ErrorEnvelope  "DUPLICATE_NAME"
// @Failure      422  {object}  ErrorEnvelope  "INVALID_INPUT"
// @Router       /areas/{id} [patch]
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	var req UpdateAreaRequest
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
// @Summary      Delete an area
// @Description  Deletes an area; its projects are cascade-deleted (their tasks survive unfiled).
// @Tags         areas
// @Param        id   path  string  true  "Area ID"
// @Security     BearerAuth
// @Success      204  "No Content"
// @Failure      404  {object}  ErrorEnvelope  "AREA_NOT_FOUND"
// @Router       /areas/{id} [delete]
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
// @Summary      Reorder areas
// @Description  Bulk-updates area displayOrder in one transaction (atomic; a foreign id rolls the whole batch back).
// @Tags         areas
// @Accept       json
// @Produce      json
// @Param        body  body      ReorderRequest  true  "New order (id → displayOrder)"
// @Security     BearerAuth
// @Success      200  {object}  ReorderResultEnvelope  "Number of rows updated"
// @Failure      404  {object}  ErrorEnvelope          "AREA_NOT_FOUND (a foreign id — whole batch rolled back)"
// @Failure      422  {object}  ErrorEnvelope          "INVALID_INPUT"
// @Router       /areas/reorder [patch]
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

func writeAppError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apperror.AppError
	if errors.As(err, &ae) {
		respond.Error(w, ae.Status, ae.Code, ae.Message)
		return
	}
	log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("unexpected internal error")
	respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "an unexpected error occurred")
}
