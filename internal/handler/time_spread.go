package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nicoflow-api/internal/middleware"
	"nicoflow-api/internal/response"
	"nicoflow-api/internal/service"
)

type TimeSpreadHandler struct {
	svc *service.TimeSpreadService
}

func NewTimeSpreadHandler(svc *service.TimeSpreadService) *TimeSpreadHandler {
	return &TimeSpreadHandler{svc: svc}
}

// Get godoc
// @Summary      Get time-spread view
// @Description  Returns tasks grouped by scheduled date for the authenticated user
// @Tags         time-spread
// @Produce      json
// @Success      200  {object}  swaggertypes.EmptyResponse
// @Failure      401  {object}  swaggertypes.ErrorResponse
// @Failure      500  {object}  swaggertypes.ErrorResponse
// @Security     BearerAuth
// @Router       /time-spread [get]
func (h *TimeSpreadHandler) Get(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	data, err := h.svc.Get(c.Request.Context(), userID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, data)
}
