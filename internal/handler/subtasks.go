package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nicoflow-api/internal/response"
	"nicoflow-api/internal/service"
)

type SubtaskHandler struct {
	svc *service.SubtaskService
}

func NewSubtaskHandler(svc *service.SubtaskService) *SubtaskHandler {
	return &SubtaskHandler{svc: svc}
}

// Create godoc
// @Summary      Create a subtask
// @Description  Creates a new subtask under the specified task
// @Tags         subtasks
// @Accept       json
// @Produce      json
// @Param        taskId  path      string                    true  "Task ID"
// @Param        body    body      swaggertypes.CreateSubtaskRequest true  "Subtask data"
// @Success      201     {object}  swaggertypes.SubtaskResponse
// @Failure      400     {object}  swaggertypes.ErrorResponse
// @Failure      401     {object}  swaggertypes.ErrorResponse
// @Failure      500     {object}  swaggertypes.ErrorResponse
// @Security     BearerAuth
// @Router       /tasks/{taskId}/subtasks [post]
func (h *SubtaskHandler) Create(c *gin.Context) {
	taskID := c.Param("taskId")
	var req struct {
		Title string `json:"title" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	s, err := h.svc.Create(c.Request.Context(), taskID, req.Title)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondCreated(c, s)
}

// List godoc
// @Summary      List subtasks
// @Description  Returns all subtasks for the specified task
// @Tags         subtasks
// @Produce      json
// @Param        taskId  path      string  true  "Task ID"
// @Success      200     {object}  swaggertypes.SubtaskListResponse
// @Failure      401     {object}  swaggertypes.ErrorResponse
// @Failure      500     {object}  swaggertypes.ErrorResponse
// @Security     BearerAuth
// @Router       /tasks/{taskId}/subtasks [get]
func (h *SubtaskHandler) List(c *gin.Context) {
	taskID := c.Param("taskId")
	subtasks, err := h.svc.ListByTask(c.Request.Context(), taskID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, subtasks)
}

// Update godoc
// @Summary      Update a subtask
// @Description  Updates the title or done state of a subtask
// @Tags         subtasks
// @Accept       json
// @Produce      json
// @Param        taskId     path      string                    true  "Task ID"
// @Param        subtaskId  path      string                    true  "Subtask ID"
// @Param        body       body      swaggertypes.UpdateSubtaskRequest true  "Updated subtask data"
// @Success      200        {object}  swaggertypes.SubtaskResponse
// @Failure      400        {object}  swaggertypes.ErrorResponse
// @Failure      401        {object}  swaggertypes.ErrorResponse
// @Failure      500        {object}  swaggertypes.ErrorResponse
// @Security     BearerAuth
// @Router       /tasks/{taskId}/subtasks/{subtaskId} [put]
func (h *SubtaskHandler) Update(c *gin.Context) {
	taskID := c.Param("taskId")
	id := c.Param("subtaskId")
	var req struct {
		Title string `json:"title"`
		Done  bool   `json:"done"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	s, err := h.svc.Update(c.Request.Context(), id, taskID, req.Done, req.Title)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, s)
}

// Delete godoc
// @Summary      Delete a subtask
// @Description  Deletes a subtask from the specified task
// @Tags         subtasks
// @Produce      json
// @Param        taskId     path      string  true  "Task ID"
// @Param        subtaskId  path      string  true  "Subtask ID"
// @Success      200        {object}  swaggertypes.EmptyResponse
// @Failure      401        {object}  swaggertypes.ErrorResponse
// @Failure      500        {object}  swaggertypes.ErrorResponse
// @Security     BearerAuth
// @Router       /tasks/{taskId}/subtasks/{subtaskId} [delete]
func (h *SubtaskHandler) Delete(c *gin.Context) {
	taskID := c.Param("taskId")
	id := c.Param("subtaskId")
	if err := h.svc.Delete(c.Request.Context(), id, taskID); err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, gin.H{})
}
