package notification

import (
	"encoding/json"
	"net/http"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// GetPreferences godoc
// @Summary      Get notification preferences
// @Description  Returns the user's notification preferences. An absent row yields the defaults ({ emailDigest:true, pushEnabled:false, smsEnabled:false, morningDigestEnabled:true, eveningDigestEnabled:true, streaksEnabled:true }). Push/SMS toggles are stored but inert until E-037.
// @Tags         notifications
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  PreferencesEnvelope  "The user's preferences"
// @Router       /notifications/preferences [get]
func (h *Handler) GetPreferences(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	view, err := h.svc.GetPreferences(r.Context(), userID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// UpdatePreferences godoc
// @Summary      Update notification preferences
// @Description  Partially or fully updates the user's notification preferences (lazy upsert). morningHour must be 5–11, eveningHour 18–22. Push/SMS toggles persist but have no delivery path until E-037.
// @Tags         notifications
// @Accept       json
// @Produce      json
// @Param        body  body      UpdatePreferences    true  "Fields to update (all optional)"
// @Security     BearerAuth
// @Success      200  {object}  PreferencesEnvelope  "The updated preferences"
// @Failure      400  {object}  ErrorEnvelope        "INVALID_INPUT (bad body or hour out of range)"
// @Router       /notifications/preferences [put]
func (h *Handler) UpdatePreferences(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	var req UpdatePreferences
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	view, err := h.svc.UpdatePreferences(r.Context(), userID, req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}
