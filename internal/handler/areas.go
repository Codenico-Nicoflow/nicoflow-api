package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/internal/response"
	"github.com/nicoflow/nicoflow-api/internal/service"
)

type AreaHandler struct {
	svc *service.AreaService
}

func NewAreaHandler(svc *service.AreaService) *AreaHandler {
	return &AreaHandler{svc: svc}
}

// Create godoc
//
//	@Summary		Create an area
//	@Description	Creates a new area for the authenticated user
//	@Tags			areas
//	@Accept			json
//	@Produce		json
//	@Param			body	body		swaggertypes.CreateAreaRequest	true	"Area data"
//	@Success		201		{object}	swaggertypes.AreaResponse
//	@Failure		400		{object}	swaggertypes.ErrorResponse
//	@Failure		401		{object}	swaggertypes.ErrorResponse
//	@Failure		500		{object}	swaggertypes.ErrorResponse
//	@Security		BearerAuth
//	@Router			/areas [post]
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

// List godoc
//
//	@Summary		List areas
//	@Description	Returns all areas belonging to the authenticated user
//	@Tags			areas
//	@Produce		json
//	@Success		200	{object}	swaggertypes.AreaListResponse
//	@Failure		401	{object}	swaggertypes.ErrorResponse
//	@Failure		500	{object}	swaggertypes.ErrorResponse
//	@Security		BearerAuth
//	@Router			/areas [get]
func (h *AreaHandler) List(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	areas, err := h.svc.List(c.Request.Context(), userID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, areas)
}

// Update godoc
//
//	@Summary		Update an area
//	@Description	Updates the name of an area owned by the authenticated user
//	@Tags			areas
//	@Accept			json
//	@Produce		json
//	@Param			id		path		string							true	"Area ID"
//	@Param			body	body		swaggertypes.UpdateAreaRequest	true	"Updated area data"
//	@Success		200		{object}	swaggertypes.AreaResponse
//	@Failure		400		{object}	swaggertypes.ErrorResponse
//	@Failure		401		{object}	swaggertypes.ErrorResponse
//	@Failure		500		{object}	swaggertypes.ErrorResponse
//	@Security		BearerAuth
//	@Router			/areas/{id} [put]
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

// Delete godoc
//
//	@Summary		Delete an area
//	@Description	Deletes an area owned by the authenticated user
//	@Tags			areas
//	@Produce		json
//	@Param			id	path		string	true	"Area ID"
//	@Success		200	{object}	swaggertypes.EmptyResponse
//	@Failure		401	{object}	swaggertypes.ErrorResponse
//	@Failure		500	{object}	swaggertypes.ErrorResponse
//	@Security		BearerAuth
//	@Router			/areas/{id} [delete]
func (h *AreaHandler) Delete(c *gin.Context) {
	id := c.Param("id")
	userID := c.GetString(middleware.ContextUserID)
	if err := h.svc.Delete(c.Request.Context(), id, userID); err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, gin.H{})
}
