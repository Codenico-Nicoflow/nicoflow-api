package googlecal

import (
	"encoding/json"
	"net/http"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// CalendarsHandler serves the calendar picker endpoints (NIC-1857).
type CalendarsHandler struct {
	svc CalendarService
}

// NewCalendarsHandler creates the picker handler.
func NewCalendarsHandler(svc CalendarService) *CalendarsHandler {
	return &CalendarsHandler{svc: svc}
}

// UpdateSelectionRequest is the body of the selection update.
//
// A pointer slice distinguishes "field omitted" from "explicitly empty": sending
// [] is a valid instruction to select nothing, while omitting the field is a
// malformed request. Collapsing the two would make an accidental empty body
// silently clear the user's selection.
type UpdateSelectionRequest struct {
	CalendarIDs *[]string `json:"calendarIds"`
}

// List handles GET /calendar/google/calendars.
//
// @Summary      List the user's Google calendars
// @Description  The calendars the user can read, each flagged with whether it currently overlays the Nicoflow calendar. A connection with no stored selection reports the primary calendar as selected, matching the overlay's default.
// @Tags         calendar
// @Produce      json
// @Success      200  {array}   CalendarView
// @Failure      409  {object}  ErrorEnvelope  "GOOGLE_NOT_CONNECTED"
// @Failure      502  {object}  ErrorEnvelope  "GOOGLE_AUTH_FAILED — Google unreachable"
// @Failure      503  {object}  ErrorEnvelope  "GOOGLE_AUTH_FAILED — integration not configured"
// @Security     BearerAuth
// @Router       /calendar/google/calendars [get]
func (h *CalendarsHandler) List(w http.ResponseWriter, r *http.Request) {
	views, err := h.svc.List(r.Context(), mw.UserIDFromCtx(r.Context()))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, views)
}

// UpdateSelection handles PUT /calendar/google/calendars.
//
// @Summary      Replace the selected calendars
// @Description  Replaces the overlay selection. Capped at 5 calendars — each selected calendar is a separate Google API call per ranged fetch. Duplicates are collapsed rather than rejected. Returns the full calendar list with the new selection applied.
// @Tags         calendar
// @Accept       json
// @Produce      json
// @Param        request  body      UpdateSelectionRequest  true  "Calendar IDs to select"
// @Success      200  {array}   CalendarView
// @Failure      409  {object}  ErrorEnvelope  "GOOGLE_NOT_CONNECTED"
// @Failure      422  {object}  ErrorEnvelope  "INVALID_INPUT — missing field, empty ID, or more than 5 calendars"
// @Security     BearerAuth
// @Router       /calendar/google/calendars [put]
func (h *CalendarsHandler) UpdateSelection(w http.ResponseWriter, r *http.Request) {
	var req UpdateSelectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "malformed request body")
		return
	}
	if req.CalendarIDs == nil {
		respond.Error(w, http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "calendarIds is required")
		return
	}

	views, err := h.svc.UpdateSelection(r.Context(), mw.UserIDFromCtx(r.Context()), *req.CalendarIDs)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, views)
}
