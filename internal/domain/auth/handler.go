package auth

import (
	"net/http"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Handler handles HTTP requests for the auth and user management domain.
type Handler struct{ svc Service }

// NewHandler creates a new auth Handler.
func NewHandler(svc Service) *Handler { return &Handler{svc: svc} }

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	respond.Error(w, http.StatusNotImplemented, apperror.ErrInternalServerError, "not implemented")
}

func (h *Handler) Register(w http.ResponseWriter, r *http.Request)       { notImplemented(w, r) }
func (h *Handler) Login(w http.ResponseWriter, r *http.Request)          { notImplemented(w, r) }
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request)        { notImplemented(w, r) }
func (h *Handler) ForgotPassword(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request)  { notImplemented(w, r) }
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request)         { notImplemented(w, r) }
func (h *Handler) LogoutAll(w http.ResponseWriter, r *http.Request)      { notImplemented(w, r) }

func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request)        { notImplemented(w, r) }
func (h *Handler) UpdateMe(w http.ResponseWriter, r *http.Request)          { notImplemented(w, r) }
func (h *Handler) DeleteMe(w http.ResponseWriter, r *http.Request)          { notImplemented(w, r) }
func (h *Handler) RegisterPushToken(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }

func (h *Handler) ListNotifications(w http.ResponseWriter, r *http.Request)    { notImplemented(w, r) }
func (h *Handler) MarkNotificationRead(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func (h *Handler) MarkAllNotificationsRead(w http.ResponseWriter, r *http.Request) {
	notImplemented(w, r)
}
func (h *Handler) DeleteNotification(w http.ResponseWriter, r *http.Request) { notImplemented(w, r) }
func (h *Handler) GetNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	notImplemented(w, r)
}
func (h *Handler) UpdateNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	notImplemented(w, r)
}
