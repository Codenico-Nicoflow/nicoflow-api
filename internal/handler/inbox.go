package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nicoflow-api/internal/middleware"
	"nicoflow-api/internal/response"
	"nicoflow-api/internal/service"
)

type InboxHandler struct {
	svc *service.InboxService
}

func NewInboxHandler(svc *service.InboxService) *InboxHandler {
	return &InboxHandler{svc: svc}
}

// Capture godoc
// @Summary      Capture to inbox
// @Description  Quickly captures a task title into the inbox (no project, no scheduling)
// @Tags         inbox
// @Accept       json
// @Produce      json
// @Param        body  body      swaggertypes.CaptureInboxRequest  true  "Task title"
// @Success      201   {object}  swaggertypes.TaskResponse
// @Failure      400   {object}  swaggertypes.ErrorResponse
// @Failure      401   {object}  swaggertypes.ErrorResponse
// @Failure      500   {object}  swaggertypes.ErrorResponse
// @Security     BearerAuth
// @Router       /inbox/capture [post]
func (h *InboxHandler) Capture(c *gin.Context) {
	var req struct {
		Title string `json:"title" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	userID := c.GetString(middleware.ContextUserID)
	task, err := h.svc.Capture(c.Request.Context(), userID, req.Title)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondCreated(c, task)
}

// List godoc
// @Summary      List inbox items
// @Description  Returns all unscheduled tasks (project_id IS NULL) for the authenticated user
// @Tags         inbox
// @Produce      json
// @Success      200  {object}  swaggertypes.TaskListResponse
// @Failure      401  {object}  swaggertypes.ErrorResponse
// @Failure      500  {object}  swaggertypes.ErrorResponse
// @Security     BearerAuth
// @Router       /inbox [get]
func (h *InboxHandler) List(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	tasks, err := h.svc.List(c.Request.Context(), userID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, tasks)
}
