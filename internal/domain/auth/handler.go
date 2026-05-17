package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Handler handles HTTP requests for the auth and user management domain.
type Handler struct {
	svc          Service
	secureCookie bool
}

// NewHandler creates a new auth Handler.
// secureCookie should be false in development (http://localhost) and true in staging/production.
func NewHandler(svc Service, secureCookie bool) *Handler {
	return &Handler{svc: svc, secureCookie: secureCookie}
}

// POST /v1/auth/register
func (h *Handler) Register(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}
	if req.Platform == "" {
		req.Platform = "web"
	}

	resp, err := h.svc.Register(r.Context(), req)
	if err != nil {
		writeAppError(w, err)
		return
	}

	h.setRefreshCookie(w, resp.RefreshToken, resp.CookieMaxAge)
	respond.JSON(w, http.StatusCreated, resp)
}

// POST /v1/auth/login
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	resp, err := h.svc.Login(r.Context(), req)
	if err != nil {
		writeAppError(w, err)
		return
	}

	h.setRefreshCookie(w, resp.RefreshToken, resp.CookieMaxAge)
	respond.JSON(w, http.StatusOK, resp)
}

// POST /v1/auth/refresh-token
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	// Cookie takes priority; fall back to JSON body.
	rawToken := ""
	if cookie, err := r.Cookie("refresh_token"); err == nil {
		rawToken = cookie.Value
	} else {
		var body struct {
			RefreshToken string `json:"refreshToken"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
			rawToken = body.RefreshToken
		}
	}

	resp, err := h.svc.RefreshToken(r.Context(), rawToken)
	if err != nil {
		writeAppError(w, err)
		return
	}

	h.setRefreshCookie(w, resp.RefreshToken, resp.CookieMaxAge)
	respond.JSON(w, http.StatusOK, resp)
}

// POST /v1/auth/logout
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	rawToken := ""
	if cookie, err := r.Cookie("refresh_token"); err == nil {
		rawToken = cookie.Value
	}

	if err := h.svc.Logout(r.Context(), userID, rawToken); err != nil {
		writeAppError(w, err)
		return
	}

	h.clearRefreshCookie(w)
	respond.NoContent(w)
}

// POST /v1/auth/logout-all
func (h *Handler) LogoutAll(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	if err := h.svc.LogoutAll(r.Context(), userID); err != nil {
		writeAppError(w, err)
		return
	}

	h.clearRefreshCookie(w)
	respond.NoContent(w)
}

// POST /v1/auth/forgot-password
func (h *Handler) ForgotPassword(w http.ResponseWriter, r *http.Request) {
	var req ForgotPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	// Always 200 — no enumeration.
	_ = h.svc.ForgotPassword(r.Context(), req.Email)
	respond.JSON(w, http.StatusOK, map[string]string{"message": "if the email exists, a reset link has been sent"})
}

// POST /v1/auth/reset-password
func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	if err := h.svc.ResetPassword(r.Context(), req); err != nil {
		writeAppError(w, err)
		return
	}

	respond.JSON(w, http.StatusOK, map[string]string{"message": "password updated successfully"})
}

// GET /v1/users/profile
func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	view, err := h.svc.GetProfile(r.Context(), userID)
	if err != nil {
		writeAppError(w, err)
		return
	}

	respond.JSON(w, http.StatusOK, view)
}

// PATCH /v1/users/me
func (h *Handler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	var req UpdateMeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	view, err := h.svc.UpdateMe(r.Context(), userID, req)
	if err != nil {
		writeAppError(w, err)
		return
	}

	respond.JSON(w, http.StatusOK, view)
}

// DELETE /v1/users/me
func (h *Handler) DeleteMe(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	if err := h.svc.DeleteMe(r.Context(), userID); err != nil {
		writeAppError(w, err)
		return
	}

	h.clearRefreshCookie(w)
	respond.NoContent(w)
}

// POST /v1/users/push-token
func (h *Handler) RegisterPushToken(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	var req RegisterPushTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}
	if req.Token == "" || (req.Platform != "ios" && req.Platform != "android") {
		respond.Error(w, http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "token and platform (ios|android) are required")
		return
	}

	if err := h.svc.RegisterPushToken(r.Context(), userID, req); err != nil {
		writeAppError(w, err)
		return
	}

	respond.JSON(w, http.StatusCreated, map[string]string{"message": "push token registered"})
}

// POST /v1/auth/biometric/register — stub for future FIDO2/WebAuthn (v2)
func (h *Handler) BiometricRegister(w http.ResponseWriter, _ *http.Request) {
	respond.Error(w, http.StatusNotImplemented, apperror.ErrInternalServerError, "biometric authentication is not yet available")
}

// POST /v1/auth/biometric/verify — stub for future FIDO2/WebAuthn (v2)
func (h *Handler) BiometricVerify(w http.ResponseWriter, _ *http.Request) {
	respond.Error(w, http.StatusNotImplemented, apperror.ErrInternalServerError, "biometric authentication is not yet available")
}

// Notification stubs — implemented in a future story.
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

// setRefreshCookie sets the HttpOnly refresh token cookie per SPEC §3.5.
// maxAge=0 produces a session cookie (no Max-Age header, expires on browser close).
// Secure is intentionally runtime-controlled (false in dev over HTTP, true in staging/production).
func (h *Handler) setRefreshCookie(w http.ResponseWriter, rawToken string, maxAge int) {
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure is intentionally dynamic: true in staging/production, false in development (HTTP)
		Name:     "refresh_token",
		Value:    rawToken,
		Path:     "/v1/auth/refresh-token",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteStrictMode,
	})
}

// clearRefreshCookie expires the refresh token cookie.
func (h *Handler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure is intentionally dynamic: true in staging/production, false in development (HTTP)
		Name:     "refresh_token",
		Value:    "",
		Path:     "/v1/auth/refresh-token",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: http.SameSiteStrictMode,
	})
}

func notImplemented(w http.ResponseWriter, _ *http.Request) {
	respond.Error(w, http.StatusNotImplemented, apperror.ErrInternalServerError, "not implemented")
}

// writeAppError converts an AppError to the correct HTTP response.
func writeAppError(w http.ResponseWriter, err error) {
	var ae *apperror.AppError
	if errors.As(err, &ae) {
		respond.Error(w, ae.Status, ae.Code, ae.Message)
		return
	}
	respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "an unexpected error occurred")
}
