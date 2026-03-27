package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nicoflow-api/internal/middleware"
	"nicoflow-api/internal/response"
	"nicoflow-api/internal/service"
)

type AISessionHandler struct {
	svc *service.AISessionService
}

func NewAISessionHandler(svc *service.AISessionService) *AISessionHandler {
	return &AISessionHandler{svc: svc}
}

func (h *AISessionHandler) Create(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	session, err := h.svc.Create(c.Request.Context(), userID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondCreated(c, session)
}

func (h *AISessionHandler) List(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	sessions, err := h.svc.List(c.Request.Context(), userID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, sessions)
}

func (h *AISessionHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString(middleware.ContextUserID)
	if err := h.svc.Delete(c.Request.Context(), id, userID); err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, gin.H{})
}
