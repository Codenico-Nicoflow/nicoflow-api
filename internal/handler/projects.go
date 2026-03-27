package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nicoflow-api/internal/middleware"
	"nicoflow-api/internal/response"
	"nicoflow-api/internal/service"
)

type ProjectHandler struct {
	svc *service.ProjectService
}

func NewProjectHandler(svc *service.ProjectService) *ProjectHandler {
	return &ProjectHandler{svc: svc}
}

func (h *ProjectHandler) Create(c *gin.Context) {
	var req struct {
		Name   string  `json:"name" binding:"required"`
		AreaID *string `json:"areaId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	userID := c.GetString(middleware.ContextUserID)
	p, err := h.svc.Create(c.Request.Context(), userID, req.Name, req.AreaID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondCreated(c, p)
}

func (h *ProjectHandler) List(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	projects, err := h.svc.List(c.Request.Context(), userID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, projects)
}

func (h *ProjectHandler) Update(c *gin.Context) {
	id := c.Param("id")
	var req struct {
		Name   string  `json:"name" binding:"required"`
		AreaID *string `json:"areaId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	userID := c.GetString(middleware.ContextUserID)
	p, err := h.svc.Update(c.Request.Context(), id, userID, req.Name, req.AreaID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, p)
}

func (h *ProjectHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString(middleware.ContextUserID)
	if err := h.svc.Delete(c.Request.Context(), id, userID); err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, gin.H{})
}
