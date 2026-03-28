package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nicoflow-api/internal/middleware"
	"nicoflow-api/internal/response"
	"nicoflow-api/internal/service"
)

type UserPlanHandler struct {
	svc *service.UserPlanService
}

func NewUserPlanHandler(svc *service.UserPlanService) *UserPlanHandler {
	return &UserPlanHandler{svc: svc}
}

// Get godoc
// @Summary      Get user plan
// @Description  Returns the current subscription plan for the authenticated user
// @Tags         user
// @Produce      json
// @Success      200  {object}  swaggertypes.UserPlanResponse
// @Failure      401  {object}  swaggertypes.ErrorResponse
// @Failure      500  {object}  swaggertypes.ErrorResponse
// @Security     BearerAuth
// @Router       /user/plan [get]
func (h *UserPlanHandler) Get(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	plan, err := h.svc.Get(c.Request.Context(), userID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, plan)
}
