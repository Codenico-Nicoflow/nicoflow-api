package billing

import (
	"net/http"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Handler handles HTTP requests for the billing domain.
type Handler struct{ svc Service }

// NewHandler creates a new billing Handler.
func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	respond.Error(w, http.StatusNotImplemented, apperror.ErrInternalServerError, "not implemented")
}

func (h *Handler) GetPlan(w http.ResponseWriter, r *http.Request)     { notImplemented(w, r) }
func (h *Handler) CheckoutURL(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func (h *Handler) PortalURL(w http.ResponseWriter, r *http.Request)   { notImplemented(w, r) }
func (h *Handler) Webhook(w http.ResponseWriter, r *http.Request)     { notImplemented(w, r) }
