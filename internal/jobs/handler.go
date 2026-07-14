package jobs

import (
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Handler exposes the internal job endpoints. These sit outside the public /v1
// contract and are guarded by the InternalToken middleware, not JWT.
type Handler struct {
	dueNotifier *DueDateNotifier
}

// NewHandler builds the jobs Handler.
func NewHandler(dueNotifier *DueDateNotifier) *Handler {
	return &Handler{dueNotifier: dueNotifier}
}

// dueNotifyResponse is the body of a successful sweep run.
type dueNotifyResponse struct {
	Generated int `json:"generated"`
}

// DueNotify runs one due-date sweep. Invoked hourly by the Render Cron Job; safe
// to call repeatedly within an hour (idempotent via dedupe_key).
func (h *Handler) DueNotify(w http.ResponseWriter, r *http.Request) {
	generated, err := h.dueNotifier.Run(r.Context())
	if err != nil {
		log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("due-date sweep failed")
		respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "sweep failed")
		return
	}
	respond.JSON(w, http.StatusOK, dueNotifyResponse{Generated: generated})
}
