package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nicoflow-api/internal/middleware"
	"nicoflow-api/internal/response"
	"nicoflow-api/internal/service"
)

type AIMessageHandler struct {
	svc *service.AIMessageService
}

func NewAIMessageHandler(svc *service.AIMessageService) *AIMessageHandler {
	return &AIMessageHandler{svc: svc}
}

func (h *AIMessageHandler) Send(c *gin.Context) {
	sessionID := c.Param("sessionId")
	var req struct {
		Content string `json:"content" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	userID := c.GetString(middleware.ContextUserID)
	msg, err := h.svc.Send(c.Request.Context(), userID, sessionID, req.Content)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondCreated(c, msg)
}

func (h *AIMessageHandler) ListBySession(c *gin.Context) {
	sessionID := c.Param("sessionId")
	userID := c.GetString(middleware.ContextUserID)
	messages, err := h.svc.ListBySession(c.Request.Context(), userID, sessionID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, messages)
}
