package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/internal/response"
	"github.com/nicoflow/nicoflow-api/internal/service"
)

type ProjectHandler struct {
	svc *service.ProjectService
}

func NewProjectHandler(svc *service.ProjectService) *ProjectHandler {
	return &ProjectHandler{svc: svc}
}

// Create godoc
//
//	@Summary		Create a project
//	@Description	Creates a new project for the authenticated user, optionally under an area
//	@Tags			projects
//	@Accept			json
//	@Produce		json
//	@Param			body	body		swaggertypes.CreateProjectRequest	true	"Project data"
//	@Success		201		{object}	swaggertypes.ProjectResponse
//	@Failure		400		{object}	swaggertypes.ErrorResponse
//	@Failure		401		{object}	swaggertypes.ErrorResponse
//	@Failure		500		{object}	swaggertypes.ErrorResponse
//	@Security		BearerAuth
//	@Router			/projects [post]
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

// List godoc
//
//	@Summary		List projects
//	@Description	Returns all projects belonging to the authenticated user
//	@Tags			projects
//	@Produce		json
//	@Success		200	{object}	swaggertypes.ProjectListResponse
//	@Failure		401	{object}	swaggertypes.ErrorResponse
//	@Failure		500	{object}	swaggertypes.ErrorResponse
//	@Security		BearerAuth
//	@Router			/projects [get]
func (h *ProjectHandler) List(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	projects, err := h.svc.List(c.Request.Context(), userID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, projects)
}

// Update godoc
//
//	@Summary		Update a project
//	@Description	Updates a project owned by the authenticated user
//	@Tags			projects
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string								true	"Project ID"
//	@Param			body	body		swaggertypes.UpdateProjectRequest	true	"Updated project data"
//	@Success		200		{object}	swaggertypes.ProjectResponse
//	@Failure		400		{object}	swaggertypes.ErrorResponse
//	@Failure		401		{object}	swaggertypes.ErrorResponse
//	@Failure		500		{object}	swaggertypes.ErrorResponse
//	@Security		BearerAuth
//	@Router			/projects/{id} [put]
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

// Delete godoc
//
//	@Summary		Delete a project
//	@Description	Deletes a project owned by the authenticated user
//	@Tags			projects
//	@Produce		json
//	@Param			id	path		string	true	"Project ID"
//	@Success		200	{object}	swaggertypes.EmptyResponse
//	@Failure		401	{object}	swaggertypes.ErrorResponse
//	@Failure		500	{object}	swaggertypes.ErrorResponse
//	@Security		BearerAuth
//	@Router			/projects/{id} [delete]
func (h *ProjectHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString(middleware.ContextUserID)
	if err := h.svc.Delete(c.Request.Context(), id, userID); err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, gin.H{})
}
