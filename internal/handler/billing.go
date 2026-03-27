package handler

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"nicoflow-api/internal/middleware"
	"nicoflow-api/internal/response"
	"nicoflow-api/internal/service"
)

type BillingHandler struct {
	svc *service.BillingService
}

func NewBillingHandler(svc *service.BillingService) *BillingHandler {
	return &BillingHandler{svc: svc}
}

func (h *BillingHandler) CheckoutURL(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	url, err := h.svc.CheckoutURL(c.Request.Context(), userID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, gin.H{"url": url})
}

func (h *BillingHandler) PortalURL(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	url, err := h.svc.PortalURL(c.Request.Context(), userID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, gin.H{"url": url})
}

func (h *BillingHandler) Webhook(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrInvalidInput, "failed to read body")
		return
	}
	sig := c.GetHeader("X-Signature")
	if err := h.svc.HandleWebhook(c.Request.Context(), body, sig); err != nil {
		response.RespondError(c, http.StatusBadRequest, response.ErrIdempotencyConflict, err.Error())
		return
	}
	response.RespondOK(c, gin.H{})
}
