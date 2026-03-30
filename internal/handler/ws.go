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

// Upgrade godoc
//
//	@Summary		WebSocket upgrade
//	@Description	Upgrades the HTTP connection to a WebSocket for real-time updates. Not yet implemented.
//	@Tags			websocket
//	@Produce		json
//	@Success		101	{string}	string	"Switching Protocols"
//	@Failure		401	{object}	swaggertypes.ErrorResponse
//	@Failure		501	{object}	swaggertypes.ErrorResponse
//	@Security		BearerAuth
//	@Router			/ws [get]
func (h *WSHandler) Upgrade(c *gin.Context) {
	// TODO: upgrade HTTP → WebSocket, register client with hub
	_ = c.GetString(middleware.ContextUserID)
	c.JSON(http.StatusNotImplemented, gin.H{"error": "websocket upgrade not yet implemented"})
}
