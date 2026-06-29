package task

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// GET /v1/tasks/{taskId}/subtasks
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

// POST /v1/tasks/{taskId}/subtasks
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

// PATCH /v1/tasks/{taskId}/subtasks/{subtaskId}
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

// DELETE /v1/tasks/{taskId}/subtasks/{subtaskId}
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
