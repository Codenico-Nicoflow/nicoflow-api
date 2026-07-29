package focus

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Handler serves the focus HTTP endpoints. It parses/validates the request, reads
// the auth claims from context, calls the service, and writes the envelope — no
// business logic.
type Handler struct {
	svc Service
}

// NewHandler creates a focus Handler.
func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

// Open handles POST /focus/sessions — starts a segment, closing any other the
// user has open.
func (h *Handler) Open(w http.ResponseWriter, r *http.Request) {
	var req OpenSessionRequest
	if err := decodeJSON(r, &req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}
	view, err := h.svc.Open(r.Context(), mw.UserIDFromCtx(r.Context()), req)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, view)
}

// Close handles POST /focus/sessions/current/close.
func (h *Handler) Close(w http.ResponseWriter, r *http.Request) {
	view, err := h.svc.CloseCurrent(r.Context(), mw.UserIDFromCtx(r.Context()))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// Heartbeat handles POST /focus/sessions/current/heartbeat. 204 with no body —
// the client already knows the segment; only last_seen moved.
func (h *Handler) Heartbeat(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Heartbeat(r.Context(), mw.UserIDFromCtx(r.Context())); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeJSON(r *http.Request, dst *OpenSessionRequest) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	if ae, ok := errors.AsType[*apperror.AppError](err); ok {
		respond.Error(w, ae.Status, ae.Code, ae.Message)
		return
	}
	log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("focus: unexpected internal error")
	respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "an unexpected error occurred")
}
