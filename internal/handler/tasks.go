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

// Create godoc
// @Summary      Create a task
// @Description  Creates a new task for the authenticated user
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        body  body      swaggertypes.CreateTaskRequest  true  "Task data"
// @Success      201   {object}  swaggertypes.TaskResponse
// @Failure      400   {object}  swaggertypes.ErrorResponse
// @Failure      401   {object}  swaggertypes.ErrorResponse
// @Failure      500   {object}  swaggertypes.ErrorResponse
// @Security     BearerAuth
// @Router       /tasks [post]
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

// List godoc
// @Summary      List tasks
// @Description  Returns all tasks belonging to the authenticated user
// @Tags         tasks
// @Produce      json
// @Success      200  {object}  swaggertypes.TaskListResponse
// @Failure      401  {object}  swaggertypes.ErrorResponse
// @Failure      500  {object}  swaggertypes.ErrorResponse
// @Security     BearerAuth
// @Router       /tasks [get]
func (h *TaskHandler) List(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	tasks, err := h.svc.List(c.Request.Context(), userID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, tasks)
}

// Update godoc
// @Summary      Update a task
// @Description  Updates a task owned by the authenticated user
// @Tags         tasks
// @Accept       json
// @Produce      json
// @Param        taskId  path      string                  true  "Task ID"
// @Param        body    body      swaggertypes.UpdateTaskRequest  true  "Updated task fields"
// @Success      200     {object}  swaggertypes.TaskResponse
// @Failure      400     {object}  swaggertypes.ErrorResponse
// @Failure      401     {object}  swaggertypes.ErrorResponse
// @Failure      500     {object}  swaggertypes.ErrorResponse
// @Security     BearerAuth
// @Router       /tasks/{taskId} [put]
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

// Delete godoc
// @Summary      Delete a task
// @Description  Deletes a task owned by the authenticated user
// @Tags         tasks
// @Produce      json
// @Param        taskId  path      string  true  "Task ID"
// @Success      200     {object}  swaggertypes.EmptyResponse
// @Failure      401     {object}  swaggertypes.ErrorResponse
// @Failure      500     {object}  swaggertypes.ErrorResponse
// @Security     BearerAuth
// @Router       /tasks/{taskId} [delete]
func (h *TaskHandler) Delete(c *gin.Context) {
	id := c.Param("taskId")
	userID := c.GetString(middleware.ContextUserID)
	if err := h.svc.Delete(c.Request.Context(), id, userID); err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, gin.H{})
}
