package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nicoflow-api/internal/middleware"
	"nicoflow-api/internal/response"
	"nicoflow-api/internal/service"
)

type AreaHandler struct {
	svc *service.AreaService
}

func NewAreaHandler(svc *service.AreaService) *AreaHandler {
	return &AreaHandler{svc: svc}
}

func (h *AreaHandler) Create(c *gin.Context) {
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	userID := c.GetString(middleware.ContextUserID)
	area, err := h.svc.Create(c.Request.Context(), userID, req.Name)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondCreated(c, area)
}

func (h *AreaHandler) List(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	areas, err := h.svc.List(c.Request.Context(), userID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, areas)
}

func (h *AreaHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Name string `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	userID := c.GetString(middleware.ContextUserID)
	area, err := h.svc.Update(c.Request.Context(), id, userID, req.Name)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, area)
}

func (h *AreaHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString(middleware.ContextUserID)
	if err := h.svc.Delete(c.Request.Context(), id, userID); err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, gin.H{})
}
