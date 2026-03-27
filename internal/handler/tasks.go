package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nicoflow-api/internal/middleware"
	"nicoflow-api/internal/model"
	"nicoflow-api/internal/response"
	"nicoflow-api/internal/service"
)

type TaskHandler struct {
	svc *service.TaskService
}

func NewTaskHandler(svc *service.TaskService) *TaskHandler {
	return &TaskHandler{svc: svc}
}

func (h *TaskHandler) Create(c *gin.Context) {
	var req struct {
		Title        string  `json:"title" binding:"required"`
		ProjectID    *string `json:"projectId"`
		DueDate      *string `json:"dueDate"`
		ScheduledFor *string `json:"scheduledFor"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	userID := c.GetString(middleware.ContextUserID)
	t, err := h.svc.Create(c.Request.Context(), userID, req.Title, req.ProjectID, req.DueDate, req.ScheduledFor)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondCreated(c, t)
}

func (h *TaskHandler) List(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	tasks, err := h.svc.List(c.Request.Context(), userID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, tasks)
}

func (h *TaskHandler) Update(c *gin.Context) {
	id := c.Param("taskId")
	var updates model.Task
	if err := c.ShouldBindJSON(&updates); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	userID := c.GetString(middleware.ContextUserID)
	t, err := h.svc.Update(c.Request.Context(), id, userID, &updates)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, t)
}

func (h *TaskHandler) Delete(c *gin.Context) {
	id := c.Param("taskId")
	userID := c.GetString(middleware.ContextUserID)
	if err := h.svc.Delete(c.Request.Context(), id, userID); err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, gin.H{})
}
