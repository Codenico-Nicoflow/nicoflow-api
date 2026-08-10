package note

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/cursorutil"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// MaxRequestBytes caps a note write. A note body is the only place in the API
// where a client posts an arbitrarily large document, so without a cap a single
// request could pin memory decoding JSON. The limit is enforced on the reader,
// not by reading the body and measuring it, so oversized input is refused before
// it is buffered.
const MaxRequestBytes int64 = 1 << 20 // 1 MiB

// Handler serves the note HTTP endpoints. It parses/validates the request, reads
// the auth claims from context, calls the service, and writes the envelope — no
// business logic.
type Handler struct {
	svc Service
}

// NewHandler creates a note Handler.
func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

// List godoc
// @Summary      List a project's notes
// @Description  Returns the project's notes ordered by updated_at DESC. Each item carries a 200-char excerpt and NO content — never render a note body from this response. Notes are Free and unlimited.
// @Tags         notes
// @Produce      json
// @Param        projectId  query     string  true  "Project to list notes for"
// @Security     BearerAuth
// @Success      200  {object}  NoteListEnvelope  "Notes with excerpts"
// @Failure      404  {object}  ErrorEnvelope     "RESOURCE_NOT_FOUND (project missing or not owned)"
// @Failure      422  {object}  ErrorEnvelope     "INVALID_INPUT (projectId missing)"
// @Router       /notes [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	cursor, limit, err := cursorutil.ParsePageParams(r)
	if err != nil {
		writeErr(w, r, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, err.Error()))
		return
	}
	result, err := h.svc.ListByProject(
		r.Context(),
		mw.UserIDFromCtx(r.Context()),
		r.URL.Query().Get("projectId"),
		ListNotesFilter{Cursor: cursor, Limit: limit},
	)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, result)
}

// Get godoc
// @Summary      Get a note
// @Description  Retrieves a single note including its full document. Cross-user access returns 404, never 403.
// @Tags         notes
// @Produce      json
// @Param        id   path      string  true  "Note ID"
// @Security     BearerAuth
// @Success      200  {object}  NoteDetailEnvelope  "The note with its content"
// @Failure      404  {object}  ErrorEnvelope       "RESOURCE_NOT_FOUND"
// @Router       /notes/{id} [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	view, err := h.svc.Get(r.Context(), mw.UserIDFromCtx(r.Context()), chi.URLParam(r, "id"))
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// Create godoc
// @Summary      Create a note
// @Description  Files a new note under a project. content is optional (defaults to an empty document). content_text is always server-derived — a client-sent value is rejected. Notes are Free and unlimited; this endpoint never returns PLAN_LIMIT_EXCEEDED. Bodies over 1 MB are refused.
// @Tags         notes
// @Accept       json
// @Produce      json
// @Param        request  body      CreateNoteRequest  true  "Note to create"
// @Security     BearerAuth
// @Success      201  {object}  NoteDetailEnvelope  "The created note"
// @Failure      404  {object}  ErrorEnvelope       "RESOURCE_NOT_FOUND (project missing or not owned)"
// @Failure      422  {object}  ErrorEnvelope       "INVALID_INPUT (bad title/content/projectId, or body over 1 MB)"
// @Router       /notes [post]
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateNoteRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	view, err := h.svc.Create(r.Context(), mw.UserIDFromCtx(r.Context()), req)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, view)
}

// Update godoc
// @Summary      Update a note
// @Description  Partial update guarded by optimistic concurrency. version is REQUIRED and must match the stored row; a stale version returns 409 and writes nothing. On 409 a client must surface a conflict state and stop autosaving — never retry silently. A successful save increments version.
// @Tags         notes
// @Accept       json
// @Produce      json
// @Param        id       path      string             true  "Note ID"
// @Param        request  body      UpdateNoteRequest  true  "Fields to change plus the version last read"
// @Security     BearerAuth
// @Success      200  {object}  NoteDetailEnvelope  "The updated note at its new version"
// @Failure      404  {object}  ErrorEnvelope       "RESOURCE_NOT_FOUND"
// @Failure      409  {object}  ErrorEnvelope       "CONFLICT (stale version — the note moved on)"
// @Failure      422  {object}  ErrorEnvelope       "INVALID_INPUT (missing version, bad title/content, or body over 1 MB)"
// @Router       /notes/{id} [patch]
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	var req UpdateNoteRequest
	if err := decodeBody(w, r, &req); err != nil {
		writeErr(w, r, err)
		return
	}
	view, err := h.svc.Update(r.Context(), mw.UserIDFromCtx(r.Context()), chi.URLParam(r, "id"), req)
	if err != nil {
		writeErr(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// Delete godoc
// @Summary      Delete a note
// @Description  Hard-deletes a note. Cross-user access returns 404, never 403.
// @Tags         notes
// @Produce      json
// @Param        id   path      string  true  "Note ID"
// @Security     BearerAuth
// @Success      204  "Deleted"
// @Failure      404  {object}  ErrorEnvelope  "RESOURCE_NOT_FOUND"
// @Router       /notes/{id} [delete]
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(r.Context(), mw.UserIDFromCtx(r.Context()), chi.URLParam(r, "id")); err != nil {
		writeErr(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// decodeBody enforces the size cap and decodes the JSON body. Both an oversized
// body and a malformed one surface as 422 INVALID_INPUT — the client cannot act
// differently on the distinction, and reporting "too large" separately would
// leak the exact threshold to a prober.
func decodeBody[T CreateNoteRequest | UpdateNoteRequest](w http.ResponseWriter, r *http.Request, dst *T) error {
	r.Body = http.MaxBytesReader(w, r.Body, MaxRequestBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "request body is too large")
		}
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "invalid request body")
	}
	return nil
}

func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	if ae, ok := errors.AsType[*apperror.AppError](err); ok {
		respond.Error(w, ae.Status, ae.Code, ae.Message)
		return
	}
	log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("note: unexpected internal error")
	respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "an unexpected error occurred")
}
