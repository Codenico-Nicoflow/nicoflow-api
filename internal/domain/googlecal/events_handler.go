package googlecal

import (
	"net/http"

	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// EventsHandler serves the Google events overlay.
type EventsHandler struct {
	svc EventsService
}

// NewEventsHandler creates the events handler.
func NewEventsHandler(svc EventsService) *EventsHandler {
	return &EventsHandler{svc: svc}
}

// List handles GET /calendar/google-events?from=&to=&refresh=.
//
// @Summary      List Google Calendar events
// @Description  Read-only overlay of the user's selected Google calendars for an inclusive date range (max 62 days). Never returns 5xx for a Google-side failure — the response carries `googleStatus` (`ok`|`disconnected`|`error`) with an empty list instead, so a Google outage cannot break the calendar view. `refresh=true` bypasses the short server-side cache.
// @Tags         calendar
// @Produce      json
// @Param        from     query     string  true   "Inclusive start date (YYYY-MM-DD)"
// @Param        to       query     string  true   "Inclusive end date (YYYY-MM-DD)"
// @Param        refresh  query     bool    false  "Bypass the cache and re-query Google"
// @Success      200  {object}  EventsResponse
// @Failure      422  {object}  ErrorEnvelope  "INVALID_INPUT (missing bound, bad date, or span > 62 days)"
// @Security     BearerAuth
// @Router       /calendar/google-events [get]
func (h *EventsHandler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	resp, err := h.svc.List(
		r.Context(),
		mw.UserIDFromCtx(r.Context()),
		q.Get("from"),
		q.Get("to"),
		// Any other value is treated as "no", so a stray ?refresh=1 cannot
		// silently defeat the cache on every poll.
		q.Get("refresh") == "true",
	)
	if err != nil {
		// The only error the service returns is a typed validation failure —
		// Google problems arrive as a status inside a 200.
		writeErr(w, r, err)
		return
	}

	respond.JSON(w, http.StatusOK, resp)
}
