package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/internal/response"
	"github.com/nicoflow/nicoflow-api/internal/service"
)

type AIMessageHandler struct {
	svc *service.AIMessageService
}

func NewAIMessageHandler(svc *service.AIMessageService) *AIMessageHandler {
	return &AIMessageHandler{svc: svc}
}

// Send godoc
//
//	@Summary		Send a message
//	@Description	Sends a message in an AI conversation session and returns the assistant reply
//	@Tags			ai
//	@Accept			json
//	@Produce		json
//	@Param			sessionId	path		string							true	"Session ID"
//	@Param			body		body		swaggertypes.SendMessageRequest	true	"Message content"
//	@Success		201			{object}	swaggertypes.AIMessageResponse
//	@Failure		400			{object}	swaggertypes.ErrorResponse
//	@Failure		401			{object}	swaggertypes.ErrorResponse
//	@Failure		500			{object}	swaggertypes.ErrorResponse
//	@Security		BearerAuth
//	@Router			/ai/sessions/{sessionId}/messages [post]
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

// ListBySession godoc
//
//	@Summary		List messages in a session
//	@Description	Returns all messages in the specified AI conversation session
//	@Tags			ai
//	@Produce		json
//	@Param			sessionId	path		string	true	"Session ID"
//	@Success		200			{object}	swaggertypes.AIMessageListResponse
//	@Failure		401			{object}	swaggertypes.ErrorResponse
//	@Failure		500			{object}	swaggertypes.ErrorResponse
//	@Security		BearerAuth
//	@Router			/ai/sessions/{sessionId}/messages [get]
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
