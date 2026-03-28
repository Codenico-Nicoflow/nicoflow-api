package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"nicoflow-api/internal/middleware"
	"nicoflow-api/internal/response"
	"nicoflow-api/internal/service"
)

type NLPHandler struct {
	svc *service.NLPService
}

func NewNLPHandler(svc *service.NLPService) *NLPHandler {
	return &NLPHandler{svc: svc}
}

// Parse godoc
// @Summary      Parse natural language input
// @Description  Parses a natural language task description into structured task fields. Requires PRO plan.
// @Tags         nlp
// @Accept       json
// @Produce      json
// @Param        body  body      swaggertypes.NLPParseRequest  true  "Natural language input"
// @Success      200   {object}  swaggertypes.NLPResponse
// @Failure      400   {object}  swaggertypes.ErrorResponse
// @Failure      401   {object}  swaggertypes.ErrorResponse
// @Failure      403   {object}  swaggertypes.ErrorResponse
// @Failure      500   {object}  swaggertypes.ErrorResponse
// @Security     BearerAuth
// @Router       /nlp [post]
func (h *NLPHandler) Parse(c *gin.Context) {
	var req struct {
		Input string `json:"input" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}

	userID := c.GetString(middleware.ContextUserID)
	result, err := h.svc.Parse(c.Request.Context(), userID, req.Input)
	if err != nil {
		if errors.Is(err, service.ErrNLPPlanRequired) {
			response.RespondError(c, http.StatusForbidden, response.ErrPlanLimitExceeded, "PRO plan required")
			return
		}
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, result)
}
