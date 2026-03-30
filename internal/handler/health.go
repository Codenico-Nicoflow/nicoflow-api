package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Health godoc
//
//	@Summary		Health check
//	@Description	Returns 200 if the server is running
//	@Tags			health
//	@Produce		json
//	@Success		200	{object}	swaggertypes.HealthResponse
//	@Router			/health [get]
func Health(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}
