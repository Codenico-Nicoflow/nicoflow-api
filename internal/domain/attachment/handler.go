package attachment

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

// Handler serves the attachment HTTP endpoints. It parses/validates the request,
// reads the auth claims from context, calls the service, and writes the
// envelope — no business logic.
type Handler struct {
	svc Service
}

// NewHandler creates an attachment Handler.
func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

// UploadURL handles POST /attachments/upload-url — Pro-gated presigned upload.
func (h *Handler) UploadURL(w http.ResponseWriter, r *http.Request) {
	var req UploadURLRequest
	if err := decodeJSON(r, &req); err != nil {
		respond.Error(w, http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "invalid request body")
		return
	}
	resp, err := h.svc.UploadURL(r.Context(), mw.UserIDFromCtx(r.Context()), mw.PlanFromCtx(r.Context()), req)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, resp)
}

// Confirm handles POST /attachments — HeadObject re-validate + guarded insert.
func (h *Handler) Confirm(w http.ResponseWriter, r *http.Request) {
	var req ConfirmRequest
	if err := decodeJSON(r, &req); err != nil {
		respond.Error(w, http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "invalid request body")
		return
	}
	view, err := h.svc.Confirm(r.Context(), mw.UserIDFromCtx(r.Context()), mw.PlanFromCtx(r.Context()), req)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, view)
}

// List handles GET /attachments?ownerType=&ownerId= — ownership-checked, no plan gate.
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ownerType := strings.TrimSpace(q.Get("ownerType"))
	ownerID := strings.TrimSpace(q.Get("ownerId"))
	if ownerType == "" || ownerID == "" {
		respond.Error(w, http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "ownerType and ownerId are required")
		return
	}
	views, err := h.svc.ListByOwner(r.Context(), mw.UserIDFromCtx(r.Context()), ownerType, ownerID)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, views)
}

// StorageUsage handles GET /attachments/usage — the caller's total stored bytes
// against the cap. No plan gate: a downgraded user still needs to see it.
func (h *Handler) StorageUsage(w http.ResponseWriter, r *http.Request) {
	usage, err := h.svc.StorageUsage(r.Context(), mw.UserIDFromCtx(r.Context()))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, usage)
}

// DownloadURL handles GET /attachments/{id}/download-url — presigned GET, no plan gate.
func (h *Handler) DownloadURL(w http.ResponseWriter, r *http.Request) {
	url, err := h.svc.DownloadURL(r.Context(), mw.UserIDFromCtx(r.Context()), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, map[string]string{"url": url})
}

// Delete handles DELETE /attachments/{id} — DB then S3, no plan gate, 204.
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
	log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("attachment: unexpected internal error")
	respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "an unexpected error occurred")
}
