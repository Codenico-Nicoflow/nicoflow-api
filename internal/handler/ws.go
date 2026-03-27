package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nicoflow-api/internal/middleware"
	"nicoflow-api/internal/service"
)

type WSHandler struct {
	svc *service.WSService
}

func NewWSHandler(svc *service.WSService) *WSHandler {
	return &WSHandler{svc: svc}
}

func (h *WSHandler) Upgrade(c *gin.Context) {
	// TODO: upgrade HTTP → WebSocket, register client with hub
	_ = c.GetString(middleware.ContextUserID)
	c.JSON(http.StatusNotImplemented, gin.H{"error": "websocket upgrade not yet implemented"})
}
