package recurrence

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Handler serves the recurrence HTTP endpoints. It parses/validates the request,
// reads the auth claims from context, calls the service, and writes the
// envelope — no business logic.
type Handler struct {
	svc Service
}

// NewHandler creates a recurrence Handler.
func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

// Create handles POST /projects/{projectId}/recurrence-rules — creates the rule
// and materializes instance #1 in the same request.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateRuleRequest
	if err := decodeJSON(r, &req); err != nil {
		respond.Error(w, http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "invalid request body")
		return
	}
	view, err := h.svc.Create(
		r.Context(),
		mw.UserIDFromCtx(r.Context()),
		chi.URLParam(r, "projectId"),
		mw.PlanFromCtx(r.Context()),
		req,
	)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, view)
}

// List handles GET /recurrence-rules with an optional ?projectId= filter.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	var projectID *string
	if v := strings.TrimSpace(r.URL.Query().Get("projectId")); v != "" {
		projectID = &v
	}
	resp, err := h.svc.List(r.Context(), mw.UserIDFromCtx(r.Context()), projectID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, resp)
}

// Get handles GET /recurrence-rules/{id}.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	view, err := h.svc.Get(r.Context(), mw.UserIDFromCtx(r.Context()), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// Update handles PATCH /recurrence-rules/{id} — edits the series template.
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req UpdateRuleRequest
	if err := decodeJSON(r, &req); err != nil {
		respond.Error(w, http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "invalid request body")
		return
	}
	view, err := h.svc.Update(r.Context(), mw.UserIDFromCtx(r.Context()), chi.URLParam(r, "id"), req)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// Pause handles PATCH /recurrence-rules/{id}/pause.
func (h *Handler) Pause(w http.ResponseWriter, r *http.Request) {
	var req PauseRequest
	if err := decodeJSON(r, &req); err != nil {
		respond.Error(w, http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "invalid request body")
		return
	}
	view, err := h.svc.SetPaused(r.Context(), mw.UserIDFromCtx(r.Context()), chi.URLParam(r, "id"), req.Paused)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// Delete handles DELETE /recurrence-rules/{id} — ends the series, 204.
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(r.Context(), mw.UserIDFromCtx(r.Context()), chi.URLParam(r, "id")); err != nil {
		writeErr(w, r, err)
		return
	}
	respond.NoContent(w)
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	if ae, ok := errors.AsType[*apperror.AppError](err); ok {
		respond.Error(w, ae.Status, ae.Code, ae.Message)
		return
	}
	log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("recurrence: unexpected internal error")
	respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "an unexpected error occurred")
}
