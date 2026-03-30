package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nicoflow-api/internal/middleware"
	"nicoflow-api/internal/response"
	"nicoflow-api/internal/service"
)

type UserHandler struct {
	svc *service.UserService
}

func NewUserHandler(svc *service.UserService) *UserHandler {
	return &UserHandler{svc: svc}
}

// DeleteMe godoc
// @Summary      Delete account
// @Description  Soft-deletes the authenticated user and revokes all their refresh tokens
// @Tags         users
// @Produce      json
// @Security     BearerAuth
// @Success      204
// @Failure      401  {object}  swaggertypes.ErrorResponse
// @Failure      500  {object}  swaggertypes.ErrorResponse
// @Router       /users/me [delete]
func (h *UserHandler) DeleteMe(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	if err := h.svc.Delete(c.Request.Context(), userID); err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrInternalServerError, "failed to delete account")
		return
	}
	c.Status(http.StatusNoContent)
}
