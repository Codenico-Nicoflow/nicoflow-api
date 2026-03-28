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

// Create godoc
// @Summary      Create an AI session
// @Description  Creates a new AI conversation session for the authenticated user
// @Tags         ai
// @Produce      json
// @Success      201  {object}  swaggertypes.AISessionResponse
// @Failure      401  {object}  swaggertypes.ErrorResponse
// @Failure      500  {object}  swaggertypes.ErrorResponse
// @Security     BearerAuth
// @Router       /ai/sessions [post]
func (h *AISessionHandler) Create(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	session, err := h.svc.Create(c.Request.Context(), userID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondCreated(c, session)
}

// List godoc
// @Summary      List AI sessions
// @Description  Returns all AI conversation sessions for the authenticated user
// @Tags         ai
// @Produce      json
// @Success      200  {object}  swaggertypes.AISessionListResponse
// @Failure      401  {object}  swaggertypes.ErrorResponse
// @Failure      500  {object}  swaggertypes.ErrorResponse
// @Security     BearerAuth
// @Router       /ai/sessions [get]
func (h *AISessionHandler) List(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	sessions, err := h.svc.List(c.Request.Context(), userID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, sessions)
}

// Delete godoc
// @Summary      Delete an AI session
// @Description  Deletes an AI conversation session owned by the authenticated user
// @Tags         ai
// @Produce      json
// @Param        id   path      string  true  "Session ID"
// @Success      200  {object}  swaggertypes.EmptyResponse
// @Failure      401  {object}  swaggertypes.ErrorResponse
// @Failure      500  {object}  swaggertypes.ErrorResponse
// @Security     BearerAuth
// @Router       /ai/sessions/{id} [delete]
func (h *AISessionHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString(middleware.ContextUserID)
	if err := h.svc.Delete(c.Request.Context(), id, userID); err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, gin.H{})
}
