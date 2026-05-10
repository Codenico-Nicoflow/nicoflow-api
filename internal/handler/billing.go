package handler

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/internal/response"
	"github.com/nicoflow/nicoflow-api/internal/service"
)

type BillingHandler struct {
	svc *service.BillingService
}

func NewBillingHandler(svc *service.BillingService) *BillingHandler {
	return &BillingHandler{svc: svc}
}

// CheckoutURL godoc
//
//	@Summary		Get checkout URL
//	@Description	Returns a Lemon Squeezy checkout URL to upgrade to PRO
//	@Tags			billing
//	@Produce		json
//	@Success		200	{object}	swaggertypes.BillingURLResponse
//	@Failure		401	{object}	swaggertypes.ErrorResponse
//	@Failure		500	{object}	swaggertypes.ErrorResponse
//	@Security		BearerAuth
//	@Router			/billing/checkout-url [get]
func (h *BillingHandler) CheckoutURL(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	url, err := h.svc.CheckoutURL(c.Request.Context(), userID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, gin.H{"url": url})
}

// PortalURL godoc
//
//	@Summary		Get billing portal URL
//	@Description	Returns a Lemon Squeezy customer portal URL to manage subscription
//	@Tags			billing
//	@Produce		json
//	@Success		200	{object}	swaggertypes.BillingURLResponse
//	@Failure		401	{object}	swaggertypes.ErrorResponse
//	@Failure		500	{object}	swaggertypes.ErrorResponse
//	@Security		BearerAuth
//	@Router			/billing/portal-url [get]
func (h *BillingHandler) PortalURL(c *gin.Context) {
	userID := c.GetString(middleware.ContextUserID)
	url, err := h.svc.PortalURL(c.Request.Context(), userID)
	if err != nil {
		response.RespondError(c, http.StatusInternalServerError, response.ErrDatabaseError, err.Error())
		return
	}
	response.RespondOK(c, gin.H{"url": url})
}

// Webhook godoc
//
//	@Summary		Lemon Squeezy webhook
//	@Description	Receives and processes Lemon Squeezy webhook events. Idempotent — duplicate events are ignored.
//	@Tags			billing
//	@Accept			json
//	@Produce		json
//	@Param			X-Signature	header		string	true	"HMAC-SHA256 signature from Lemon Squeezy"
//	@Success		200			{object}	swaggertypes.EmptyResponse
//	@Failure		400			{object}	swaggertypes.ErrorResponse
//	@Failure		409			{object}	swaggertypes.ErrorResponse
//	@Router			/billing/webhook [post]
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
