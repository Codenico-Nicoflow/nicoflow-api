package task

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// ListSubtasks godoc
// @Summary      List subtasks
// @Description  Lists a task's subtasks, ordered by position. Parent-task ownership is enforced.
// @Tags         subtasks
// @Produce      json
// @Param        taskId  path      string  true  "Parent task ID"
// @Security     BearerAuth
// @Success      200  {object}  SubtaskListEnvelope  "List of subtasks"
// @Failure      404  {object}  ErrorEnvelope        "RESOURCE_NOT_FOUND (task not found / not owned)"
// @Router       /tasks/{taskId}/subtasks [get]
func (h *Handler) ListSubtasks(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	taskID := chi.URLParam(r, "taskId")

	resp, err := h.subtaskSvc.List(r.Context(), userID, taskID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, resp)
}

// CreateSubtask godoc
// @Summary      Create a subtask
// @Description  Creates a subtask under a task. Appends to the end unless position is given.
// @Tags         subtasks
// @Accept       json
// @Produce      json
// @Param        taskId  path      string                true  "Parent task ID"
// @Param        body    body      CreateSubtaskRequest  true  "Subtask (title required, position optional)"
// @Security     BearerAuth
// @Success      201  {object}  SubtaskEnvelope  "The created subtask"
// @Failure      404  {object}  ErrorEnvelope    "RESOURCE_NOT_FOUND (task not found / not owned)"
// @Failure      422  {object}  ErrorEnvelope    "INVALID_INPUT"
// @Router       /tasks/{taskId}/subtasks [post]
func (h *Handler) CreateSubtask(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	taskID := chi.URLParam(r, "taskId")

	var req CreateSubtaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	view, err := h.subtaskSvc.Create(r.Context(), userID, taskID, req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, view)
}

// UpdateSubtask godoc
// @Summary      Update a subtask
// @Description  Partial update of a subtask (title, done, position). Parent-task ownership enforced.
// @Tags         subtasks
// @Accept       json
// @Produce      json
// @Param        taskId     path      string                true  "Parent task ID"
// @Param        subtaskId  path      string                true  "Subtask ID"
// @Param        body       body      UpdateSubtaskRequest  true  "Fields to update (all optional)"
// @Security     BearerAuth
// @Success      200  {object}  SubtaskEnvelope  "The updated subtask"
// @Failure      404  {object}  ErrorEnvelope    "RESOURCE_NOT_FOUND"
// @Failure      422  {object}  ErrorEnvelope    "INVALID_INPUT"
// @Router       /tasks/{taskId}/subtasks/{subtaskId} [patch]
func (h *Handler) UpdateSubtask(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	taskID := chi.URLParam(r, "taskId")
	id := chi.URLParam(r, "subtaskId")

	var req UpdateSubtaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	view, err := h.subtaskSvc.Update(r.Context(), userID, taskID, id, req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// DeleteSubtask godoc
// @Summary      Delete a subtask
// @Description  Deletes a subtask. Parent-task ownership enforced.
// @Tags         subtasks
// @Param        taskId     path  string  true  "Parent task ID"
// @Param        subtaskId  path  string  true  "Subtask ID"
// @Security     BearerAuth
// @Success      204  "No Content"
// @Failure      404  {object}  ErrorEnvelope  "RESOURCE_NOT_FOUND"
// @Router       /tasks/{taskId}/subtasks/{subtaskId} [delete]
func (h *Handler) DeleteSubtask(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	taskID := chi.URLParam(r, "taskId")
	id := chi.URLParam(r, "subtaskId")

	if err := h.subtaskSvc.Delete(r.Context(), userID, taskID, id); err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.NoContent(w)
}
