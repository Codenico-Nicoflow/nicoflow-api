package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"nicoflow-api/internal/middleware"
	"nicoflow-api/internal/response"
	"nicoflow-api/internal/service"
)

type AttachmentHandler struct {
	svc *service.AttachmentService
}

func NewAttachmentHandler(svc *service.AttachmentService) *AttachmentHandler {
	return &AttachmentHandler{svc: svc}
}

// UploadURL godoc
//
//	@Summary		Get S3 upload URL
//	@Description	Returns a pre-signed S3 URL to upload a file attachment
//	@Tags			attachments
//	@Accept			json
//	@Produce		json
//	@Param			body	body		swaggertypes.UploadURLRequest	true	"File name to upload"
//	@Success		200		{object}	swaggertypes.URLResponse
//	@Failure		400		{object}	swaggertypes.ErrorResponse
//	@Failure		401		{object}	swaggertypes.ErrorResponse
//	@Failure		500		{object}	swaggertypes.ErrorResponse
//	@Security		BearerAuth
//	@Router			/attachments/upload-url [post]
func (h *AttachmentHandler) UploadURL(c *gin.Context) {
	var req struct {
		Filename string `json:"filename" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	userID := c.GetString(middleware.ContextUserID)
	url, err := h.svc.UploadURL(c.Request.Context(), userID, req.Filename)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, gin.H{"url": url})
}

// DownloadURL godoc
//
//	@Summary		Get S3 download URL
//	@Description	Returns a pre-signed S3 URL to download a file attachment
//	@Tags			attachments
//	@Accept			json
//	@Produce		json
//	@Param			body	body		swaggertypes.DownloadURLRequest	true	"S3 object key"
//	@Success		200		{object}	swaggertypes.URLResponse
//	@Failure		400		{object}	swaggertypes.ErrorResponse
//	@Failure		401		{object}	swaggertypes.ErrorResponse
//	@Failure		500		{object}	swaggertypes.ErrorResponse
//	@Security		BearerAuth
//	@Router			/attachments/download-url [post]
func (h *AttachmentHandler) DownloadURL(c *gin.Context) {
	var req struct {
		Key string `json:"key" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, err.Error())
		return
	}
	userID := c.GetString(middleware.ContextUserID)
	url, err := h.svc.DownloadURL(c.Request.Context(), userID, req.Key)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, gin.H{"url": url})
}
