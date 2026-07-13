package notification

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Handler handles HTTP requests for the notification domain.
type Handler struct{ svc Service }

// NewHandler creates a new notification Handler.
func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

// List godoc
// @Summary      List notifications
// @Description  Lists the user's notifications, cursor-paginated, newest first, with an optional read filter.
// @Tags         notifications
// @Produce      json
// @Param        isRead  query     bool    false  "Filter by read state"
// @Param        limit   query     int     false  "Page size (1–100)"
// @Param        cursor  query     string  false  "Pagination cursor"
// @Security     BearerAuth
// @Success      200  {object}  NotificationListEnvelope  "Paginated notifications"
// @Failure      400  {object}  ErrorEnvelope             "INVALID_INPUT (bad limit/cursor)"
// @Router       /notifications [get]
func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	f := ListNotificationsFilter{Cursor: r.URL.Query().Get("cursor")}

	if v := r.URL.Query().Get("isRead"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "isRead must be a boolean")
			return
		}
		f.IsRead = &b
	}

	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		l, err := strconv.Atoi(lStr)
		if err != nil || l < 1 || l > 100 {
			respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "limit must be an integer between 1 and 100")
			return
		}
		f.Limit = l
	}

	resp, err := h.svc.List(r.Context(), userID, f)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, resp)
}

// UnreadCount godoc
// @Summary      Unread notification count
// @Description  Returns the number of unread notifications — the cheap poll target for the bell badge.
// @Tags         notifications
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  UnreadCountEnvelope  "Unread count"
// @Router       /notifications/unread-count [get]
func (h *Handler) UnreadCount(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	resp, err := h.svc.UnreadCount(r.Context(), userID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, resp)
}

// MarkRead godoc
// @Summary      Mark a notification read
// @Description  Marks a single notification read. Cross-user access returns 404.
// @Tags         notifications
// @Produce      json
// @Param        id   path      string  true  "Notification ID"
// @Security     BearerAuth
// @Success      200  {object}  NotificationEnvelope  "The updated notification"
// @Failure      404  {object}  ErrorEnvelope         "NOTIFICATION_NOT_FOUND"
// @Router       /notifications/{id}/read [patch]
func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	view, err := h.svc.MarkRead(r.Context(), userID, id)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, view)
}

// MarkAllRead godoc
// @Summary      Mark all notifications read
// @Description  Marks every unread notification read and returns how many were updated.
// @Tags         notifications
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  CountEnvelope  "Number marked read"
// @Router       /notifications/read-all [patch]
func (h *Handler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	resp, err := h.svc.MarkAllRead(r.Context(), userID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.JSON(w, http.StatusOK, resp)
}

// Delete godoc
// @Summary      Dismiss a notification
// @Description  Hard-deletes a notification. Cross-user access returns 404.
// @Tags         notifications
// @Param        id   path  string  true  "Notification ID"
// @Security     BearerAuth
// @Success      204  "No Content"
// @Failure      404  {object}  ErrorEnvelope  "NOTIFICATION_NOT_FOUND"
// @Router       /notifications/{id} [delete]
func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	id := chi.URLParam(r, "id")

	if err := h.svc.Delete(r.Context(), userID, id); err != nil {
		writeAppError(w, r, err)
		return
	}
	respond.NoContent(w)
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
