package task

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Handler handles HTTP requests for the task domain (including subtasks).
type Handler struct {
	svc        Service
	subtaskSvc SubtaskService
}

// NewHandler creates a new task Handler.
func NewHandler(svc Service, subtaskSvc SubtaskService) *Handler {
	return &Handler{svc: svc, subtaskSvc: subtaskSvc}
}

// ListByProject godoc
// @Summary      List tasks in a project
// @Description  Lists the tasks within a project, with optional filtering, sorting and case-insensitive ILIKE search over title+notes.
// @Tags         tasks
// @Produce      json
// @Param        projectId  path      string  true   "Project ID"
// @Param        status     query     string  false  "Filter by status (inbox|active|someday|done|cancelled)"
// @Param        priority   query     string  false  "Filter by priority (low|medium|high)"
// @Param        energy     query     string  false  "Filter by energy (low|medium|deep)"
// @Param        search     query     string  false  "ILIKE search over title + notes"
// @Param        sortField  query     string  false  "Sort field (displayOrder|scheduledFor|priority|title|createdAt|energy)"
// @Param        sortOrder  query     string  false  "Sort order (asc|desc)"
// @Security     BearerAuth
// @Success      200  {object}  TaskListEnvelope  "List of tasks"
// @Failure      400  {object}  ErrorEnvelope     "INVALID_INPUT (bad sortField/sortOrder or filter)"
// @Failure      404  {object}  ErrorEnvelope     "PROJECT_NOT_FOUND"
// @Router       /projects/{projectId}/tasks [get]
func (h *Handler) ListByProject(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	projectID := chi.URLParam(r, "projectId")

	resp, err := h.svc.ListByProject(r.Context(), userID, projectID, parseTaskListFilter(r))
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, resp)
}

// parseTaskListFilter reads filter/sort/search query params. Value validation
// (allowed enums, sort whitelist) happens in the service/query builder.
func parseTaskListFilter(r *http.Request) ListTasksFilter {
	q := r.URL.Query()
	f := ListTasksFilter{
		Search:    q.Get("search"),
		SortField: q.Get("sortField"),
		SortOrder: q.Get("sortOrder"),
	}
	if v := q.Get("status"); v != "" {
		f.Status = &v
	}
	if v := q.Get("priority"); v != "" {
		f.Priority = &v
	}
	if v := q.Get("energy"); v != "" {
		f.Energy = &v
	}
	return f
}

// Get godoc
// @Summary      Get a task
// @Description  Retrieves a single task by ID. Cross-user access returns 404 (no existence leak).
// @Tags         tasks
// @Produce      json
// @Param        id   path      string  true  "Task ID"
// @Security     BearerAuth
// @Success      200  {object}  TaskEnvelope   "The task"
// @Failure      404  {object}  ErrorEnvelope  "TASK_NOT_FOUND"
// @Router       /tasks/{id} [get]
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
// @Summary      Create a task
// @Description  Creates a task in a project. Title-only is valid (quick-add); other fields default server-side (status=inbox, priority=medium, energy=medium, rollsOver=true). Free plan allows 50 active+inbox tasks per project.
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        projectId  path      string             true  "Project ID"
// @Param        body       body      CreateTaskRequest  true  "Task to create (only title is required)"
// @Security     BearerAuth
// @Success      201  {object}  TaskEnvelope   "The created task"
// @Failure      403  {object}  ErrorEnvelope  "PLAN_LIMIT_EXCEEDED"
// @Failure      404  {object}  ErrorEnvelope  "PROJECT_NOT_FOUND"
// @Failure      422  {object}  ErrorEnvelope  "INVALID_INPUT / INVALID_STATUS / INVALID_PRIORITY"
// @Router       /projects/{projectId}/tasks [post]
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

// Update godoc
// @Summary      Update a task
// @Description  Partial update of any mutable field. status→done sets completedAt server-side; moving away from done clears it. A PATCH that moves a task into active/inbox is subject to the plan limit.
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        id    path      string             true  "Task ID"
// @Param        body  body      UpdateTaskRequest  true  "Fields to update (all optional)"
// @Security     BearerAuth
// @Success      200  {object}  TaskEnvelope   "The updated task"
// @Failure      403  {object}  ErrorEnvelope  "PLAN_LIMIT_EXCEEDED"
// @Failure      404  {object}  ErrorEnvelope  "TASK_NOT_FOUND"
// @Failure      422  {object}  ErrorEnvelope  "INVALID_INPUT / INVALID_STATUS / INVALID_PRIORITY"
// @Router       /tasks/{id} [patch]
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

// Delete godoc
// @Summary      Delete a task
// @Description  Hard-deletes a task; its subtasks cascade.
// @Tags         tasks
// @Param        id   path  string  true  "Task ID"
// @Security     BearerAuth
// @Success      204  "No Content"
// @Failure      404  {object}  ErrorEnvelope  "TASK_NOT_FOUND"
// @Router       /tasks/{id} [delete]
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.svc.Delete(r.Context(), userID, id); err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.NoContent(w)
}

// SetStatus godoc
// @Summary      Set task status (shorthand)
// @Description  Status-only update (checkbox toggle, move to someday). Same completedAt side-effects and plan-limit semantics as the full PATCH.
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        id    path      string            true  "Task ID"
// @Param        body  body      SetStatusRequest  true  "New status"
// @Security     BearerAuth
// @Success      200  {object}  TaskEnvelope   "The updated task"
// @Failure      403  {object}  ErrorEnvelope  "PLAN_LIMIT_EXCEEDED"
// @Failure      404  {object}  ErrorEnvelope  "TASK_NOT_FOUND"
// @Failure      422  {object}  ErrorEnvelope  "INVALID_INPUT / INVALID_STATUS"
// @Router       /tasks/{id}/status [patch]
func (h *Handler) SetStatus(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	plan := mw.PlanFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	var req SetStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	view, err := h.svc.SetStatus(r.Context(), userID, id, plan, req.Status)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// Schedule godoc
// @Summary      Schedule a task (soft)
// @Description  Sets the soft scheduledFor intention + rollsOver flag. scheduledFor null (or absent) unschedules. A bad date returns 400 INVALID_DATE.
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        id    path      string           true  "Task ID"
// @Param        body  body      ScheduleRequest  true  "scheduledFor (ISO date or null) + optional rollsOver"
// @Security     BearerAuth
// @Success      200  {object}  TaskEnvelope   "The updated task"
// @Failure      400  {object}  ErrorEnvelope  "INVALID_DATE"
// @Failure      404  {object}  ErrorEnvelope  "TASK_NOT_FOUND"
// @Router       /tasks/{id}/schedule [patch]
func (h *Handler) Schedule(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	var req ScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	view, err := h.svc.Schedule(r.Context(), userID, id, req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// ReorderOne godoc
// @Summary      Reorder a task
// @Description  Moves a task to a target displayOrder; siblings in the project are repacked to a contiguous 0..n-1 sequence (transactional).
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        id    path      string             true  "Task ID"
// @Param        body  body      ReorderOneRequest  true  "Target displayOrder"
// @Security     BearerAuth
// @Success      200  {object}  TaskEnvelope   "The moved task"
// @Failure      404  {object}  ErrorEnvelope  "TASK_NOT_FOUND"
// @Failure      422  {object}  ErrorEnvelope  "INVALID_INPUT"
// @Router       /tasks/{id}/reorder [patch]
func (h *Handler) ReorderOne(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	var req ReorderOneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	view, err := h.svc.ReorderOne(r.Context(), userID, id, req.DisplayOrder)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// Focus godoc
// @Summary      Focus — what can I do right now?
// @Description  Returns a deterministically-ranked short list of the user's active+inbox tasks (across all projects) that fit the given time/energy. Scoring: energy match, time-budget fit (over-budget excluded), due/overdue escalation, scheduled proximity, priority tiebreak.
// @Tags         tasks
// @Produce      json
// @Param        available  query     int     false  "Minutes available (over-budget tasks excluded)"
// @Param        energy     query     string  false  "Current energy (low|medium|deep)"
// @Param        limit      query     int     false  "Max results (default 5, max 20)"
// @Security     BearerAuth
// @Success      200  {object}  TaskListEnvelope  "Ranked tasks"
// @Failure      400  {object}  ErrorEnvelope     "INVALID_INPUT (bad available/energy/limit)"
// @Router       /focus [get]
func (h *Handler) Focus(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	q := r.URL.Query()

	p := FocusParams{Energy: q.Get("energy")}
	if v := q.Get("available"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "available must be an integer")
			return
		}
		p.Available = n
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "limit must be an integer")
			return
		}
		p.Limit = n
	}

	resp, err := h.svc.Focus(r.Context(), userID, p)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, resp)
}

// TimeSpread godoc
// @Summary      Time spread (today / tomorrow / this week)
// @Description  Buckets the user's active+inbox tasks into today/tomorrow/thisWeek with the no-guilt roll-forward (a past soft-scheduled task with rollsOver=true carries to today instead of going overdue). someday/done/cancelled are excluded.
// @Tags         tasks
// @Produce      json
// @Security     BearerAuth
// @Param        tz   query     string  false  "IANA timezone for day bucketing (e.g. Asia/Jerusalem); defaults to UTC"
// @Success      200  {object}  TimeSpreadEnvelope  "Three-bucket task lists"
// @Failure      400  {object}  ErrorEnvelope  "INVALID_INPUT — unknown timezone"
// @Router       /time-spread [get]
func (h *Handler) TimeSpread(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	loc, err := parseTimezone(r.URL.Query().Get("tz"))
	if err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "unknown timezone")
		return
	}

	resp, err := h.svc.TimeSpread(r.Context(), userID, loc)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, resp)
}

// parseTimezone resolves an IANA tz name to a *time.Location. Empty → UTC.
// An unknown zone returns an error the caller maps to 400 INVALID_INPUT.
func parseTimezone(tz string) (*time.Location, error) {
	if tz == "" {
		return time.UTC, nil
	}
	return time.LoadLocation(tz)
}

// ── not yet implemented (later E-013 stories) ────────────────────────────────

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	respond.Error(w, http.StatusNotImplemented, apperror.ErrInternalServerError, "not implemented")
}

func (h *Handler) Search(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }

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
