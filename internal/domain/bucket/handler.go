package bucket

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Handler handles HTTP requests for the bucket (inbox) domain.
type Handler struct{ svc Service }

// NewHandler creates a new bucket Handler.
func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

// List godoc
// @Summary      List inbox items
// @Description  Lists the caller's bucket items, newest first. Processed and unprocessed items are both returned; the client partitions them into Inbox / Archived.
// @Tags         bucket
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  BucketListResponse
// @Router       /bucket [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	resp, err := h.svc.List(r.Context(), userID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, resp)
}

// Get godoc
// @Summary      Get an inbox item
// @Description  Retrieves a single bucket item by ID. Cross-user access returns 404 (no existence leak).
// @Tags         bucket
// @Produce      json
// @Param        id   path      string  true  "Bucket item ID"
// @Security     BearerAuth
// @Success      200  {object}  BucketView
// @Failure      404 "RESOURCE_NOT_FOUND"
// @Router       /bucket/{id} [get]
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	view, err := h.svc.Get(r.Context(), userID, id)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// Create godoc
// @Summary      Capture an inbox item
// @Description  Quick-captures a thought into the bucket. Content is 1–500 characters (trimmed).
// @Tags         bucket
// @Accept       json
// @Produce      json
// @Param        body  body      CreateBucketRequest  true  "Content to capture"
// @Security     BearerAuth
// @Success      201  {object}  BucketView
// @Failure      422 "INVALID_INPUT"
// @Router       /bucket [post]
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	var req CreateBucketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	view, err := h.svc.Create(r.Context(), userID, req.Content)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusCreated, view)
}

// Update godoc
// @Summary      Edit an unprocessed inbox item
// @Description  Edits the content of an unprocessed bucket item. Editing a processed item returns 409.
// @Tags         bucket
// @Accept       json
// @Produce      json
// @Param        id    path      string               true  "Bucket item ID"
// @Param        body  body      UpdateBucketRequest  true  "New content"
// @Security     BearerAuth
// @Success      200  {object}  BucketView
// @Failure      404 "RESOURCE_NOT_FOUND"
// @Failure      409 "CONFLICT (already processed)"
// @Failure      422 "INVALID_INPUT"
// @Router       /bucket/{id} [patch]
func (h *Handler) Update(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	var req UpdateBucketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	view, err := h.svc.Update(r.Context(), userID, id, req.Content)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// Delete godoc
// @Summary      Delete an inbox item
// @Description  Hard-deletes a bucket item regardless of processed state.
// @Tags         bucket
// @Param        id   path  string  true  "Bucket item ID"
// @Security     BearerAuth
// @Success      204  "No Content"
// @Failure      404 "RESOURCE_NOT_FOUND"
// @Router       /bucket/{id} [delete]
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.svc.Delete(r.Context(), userID, id); err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.NoContent(w)
}

// Process godoc
// @Summary      Process an inbox item
// @Description  Converts an unprocessed item into a task (requires projectId + taskDetails), trashes it, or (note — not yet implemented) returns 501. Already-processed items return 409.
// @Tags         bucket
// @Accept       json
// @Produce      json
// @Param        id    path      string                true  "Bucket item ID"
// @Param        body  body      ProcessBucketRequest  true  "How to process the item"
// @Security     BearerAuth
// @Success      200  {object}  BucketView
// @Failure      404 "RESOURCE_NOT_FOUND / PROJECT_NOT_FOUND"
// @Failure      403 "PLAN_LIMIT_EXCEEDED"
// @Failure      409 "CONFLICT (already processed)"
// @Failure      422 "INVALID_INPUT"
// @Failure      501 "note processing not implemented"
// @Router       /bucket/{id}/process [post]
func (h *Handler) Process(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	plan := mw.PlanFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	var req ProcessBucketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	view, err := h.svc.Process(r.Context(), userID, id, plan, req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

func writeAppError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apperror.AppError
	if errors.As(err, &ae) {
		respond.Error(w, ae.Status, ae.Code, ae.Message)
		return
	}
	log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("unexpected internal error")
	respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "an unexpected error occurred")
}
