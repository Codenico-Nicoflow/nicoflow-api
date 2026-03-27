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

func (h *TimeSpreadHandler) Get(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	data, err := h.svc.Get(c.Request.Context(), userID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, data)
}
