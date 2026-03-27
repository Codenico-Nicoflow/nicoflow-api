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

func (h *InboxHandler) List(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	tasks, err := h.svc.List(c.Request.Context(), userID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, tasks)
}
