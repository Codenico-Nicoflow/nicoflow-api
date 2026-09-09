package notification

import (
	"encoding/json"
	"net/http"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Subscribe godoc
// @Summary      Subscribe to web push
// @Description  Stores a push subscription for the user (Pro-only). Discriminated on `platform`: web (default) sends endpoint+keys, expo sends expoPushToken. Upserts. Free-plan callers get PLAN_LIMIT_EXCEEDED.
// @Tags         notifications
// @Accept       json
// @Param        body  body      SubscribeRequest  true  "Push subscription (web: endpoint + keys; expo: expoPushToken)"
// @Security     BearerAuth
// @Success      201  "Subscription stored"
// @Failure      403  {object}  ErrorEnvelope  "PLAN_LIMIT_EXCEEDED (free plan)"
// @Failure      422  {object}  ErrorEnvelope  "INVALID_INPUT (missing fields for the platform)"
// @Router       /notifications/push/subscribe [post]
func (h *Handler) Subscribe(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	plan := mw.PlanFromCtx(r.Context())

	var req SubscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	if err := h.svc.Subscribe(r.Context(), userID, plan, req); err != nil {
		writeAppError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
}

// Unsubscribe godoc
// @Summary      Unsubscribe from web push
// @Description  Removes the user's push subscription by endpoint (web) or expoPushToken (mobile). Idempotent.
// @Tags         notifications
// @Accept       json
// @Param        body  body      SubscribeRequest  true  "Endpoint or expoPushToken to remove"
// @Security     BearerAuth
// @Success      204  "Subscription removed"
// @Router       /notifications/push/subscribe [delete]
func (h *Handler) Unsubscribe(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	var req SubscribeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	if err := h.svc.Unsubscribe(r.Context(), userID, req); err != nil {
		writeAppError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
