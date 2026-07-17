package auth

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Handler handles HTTP requests for the auth and user management domain.
type Handler struct {
	svc          Service
	secureCookie bool
	crossSite    bool
}

// HandlerConfig configures cookie behaviour for the auth Handler.
//   - SecureCookie: false in development (http://localhost), true in staging/production.
//   - CrossSite: true when the frontend and API are on different registrable domains
//     (e.g. *.vercel.app → *.onrender.com), which requires SameSite=None. SameSite=None
//     demands Secure, so CrossSite is only honoured when SecureCookie is also true.
type HandlerConfig struct {
	SecureCookie bool
	CrossSite    bool
}

// NewHandler creates a new auth Handler.
func NewHandler(svc Service, cfg HandlerConfig) *Handler {
	return &Handler{
		svc:          svc,
		secureCookie: cfg.SecureCookie,
		// SameSite=None is invalid without Secure; ignore CrossSite over plain HTTP so a
		// misconfiguration can't produce a cookie the browser silently rejects.
		crossSite: cfg.CrossSite && cfg.SecureCookie,
	}
}

// Register godoc
// @Summary      Register a new account
// @Description  Creates a user and sends a verification email. Does not log the user in — no token is issued and no cookie is set; the user must verify (when enforced) then log in.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      RegisterRequest  true  "Registration details"
// @Success      201   {object}  AuthEnvelope     "User created (token empty, no session)"
// @Failure      409   {object}  ErrorEnvelope    "EMAIL_ALREADY_EXISTS or USERNAME_ALREADY_EXISTS"
// @Failure      400   {object}  ErrorEnvelope    "WEAK_PASSWORD"
// @Failure      422   {object}  ErrorEnvelope    "INVALID_INPUT or INVALID_EMAIL"
// @Failure      429   {object}  ErrorEnvelope    "RATE_LIMITED"
// @Router       /auth/register [post]
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
		writeAppError(w, r, err)
		return
	}

	// Register no longer establishes a session — only set the cookie if a refresh
	// token was issued (it isn't, while verification-before-login is the flow).
	if resp.RefreshToken != "" {
		h.setRefreshCookie(w, resp.RefreshToken, resp.CookieMaxAge)
	}
	respond.JSON(w, http.StatusCreated, resp)
}

// Login godoc
// @Summary      Log in
// @Description  Authenticates by email or username and returns a JWT access token plus an HttpOnly refresh cookie. When REQUIRE_EMAIL_VERIFICATION is enabled, an unverified account is rejected with 403 EMAIL_NOT_VERIFIED after the password check (so verification state is never leaked to a wrong password).
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      LoginRequest  true  "Credentials (identifier = email or username)"
// @Success      200   {object}  AuthEnvelope   "Access token + user; Set-Cookie refresh token"
// @Failure      401   {object}  ErrorEnvelope  "UNAUTHORIZED (invalid credentials; identical for unknown user and wrong password)"
// @Failure      403   {object}  ErrorEnvelope  "EMAIL_NOT_VERIFIED"
// @Failure      422   {object}  ErrorEnvelope  "INVALID_INPUT"
// @Failure      429   {object}  ErrorEnvelope  "RATE_LIMITED (IP limit or account lockout)"
// @Router       /auth/login [post]
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	resp, err := h.svc.Login(r.Context(), req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	h.setRefreshCookie(w, resp.RefreshToken, resp.CookieMaxAge)
	respond.JSON(w, http.StatusOK, resp)
}

// Refresh godoc
// @Summary      Refresh the access token
// @Description  Rotates the refresh token (delete-old/insert-new) and issues a fresh access token. The raw refresh token is read from the HttpOnly cookie (preferred) or a JSON body. Replaying a consumed token is reuse and revokes all of the user's sessions.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Success      200  {object}  AuthEnvelope   "New access token + rotated refresh cookie"
// @Failure      401  {object}  ErrorEnvelope  "INVALID_TOKEN (missing, expired, tampered, or already-used)"
// @Router       /auth/refresh-token [post]
func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	// Cookie takes priority; fall back to JSON body.
	rawToken := ""
	if cookie, err := r.Cookie(h.refreshCookieName()); err == nil {
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
		writeAppError(w, r, err)
		return
	}

	h.setRefreshCookie(w, resp.RefreshToken, resp.CookieMaxAge)
	respond.JSON(w, http.StatusOK, resp)
}

// Logout godoc
// @Summary      Log out (current session)
// @Description  Revokes the single refresh token carried by the cookie and clears it. Authenticates off the HttpOnly refresh cookie, not the access token. Idempotent — a missing or already-deleted token still returns 204.
// @Tags         auth
// @Produce      json
// @Success      204  "Session ended; refresh cookie cleared"
// @Router       /auth/logout [post]
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())
	rawToken := ""
	if cookie, err := r.Cookie(h.refreshCookieName()); err == nil {
		rawToken = cookie.Value
	}

	if err := h.svc.Logout(r.Context(), userID, rawToken); err != nil {
		writeAppError(w, r, err)
		return
	}

	h.clearRefreshCookie(w)
	respond.NoContent(w)
}

// LogoutAll godoc
// @Summary      Log out of all devices
// @Description  Revokes every refresh token for the authenticated user. Requires a valid access token (revokes by the userID claim).
// @Tags         auth
// @Produce      json
// @Security     BearerAuth
// @Success      204  "All sessions revoked; refresh cookie cleared"
// @Failure      401  {object}  ErrorEnvelope  "UNAUTHORIZED (missing or invalid access token)"
// @Router       /auth/logout-all [post]
func (h *Handler) LogoutAll(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	if err := h.svc.LogoutAll(r.Context(), userID); err != nil {
		writeAppError(w, r, err)
		return
	}

	h.clearRefreshCookie(w)
	respond.NoContent(w)
}

// ForgotPassword godoc
// @Summary      Request a password reset
// @Description  Sends a password-reset email. Always returns 200 regardless of whether the email exists (no user enumeration).
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      ForgotPasswordRequest  true  "Account email"
// @Success      200   {object}  MessageEnvelope
// @Failure      429   {object}  ErrorEnvelope  "RATE_LIMITED"
// @Router       /auth/forgot-password [post]
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

// ResetPassword godoc
// @Summary      Reset password with a token
// @Description  Sets a new password using the single-use reset token from the email. Revokes all active refresh tokens on success.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      ResetPasswordRequest  true  "Reset token + new password"
// @Success      200   {object}  MessageEnvelope
// @Failure      401   {object}  ErrorEnvelope  "INVALID_TOKEN (invalid, expired, or already-used)"
// @Failure      400   {object}  ErrorEnvelope  "WEAK_PASSWORD"
// @Failure      422   {object}  ErrorEnvelope  "INVALID_INPUT (e.g. password mismatch)"
// @Router       /auth/reset-password [post]
func (h *Handler) ResetPassword(w http.ResponseWriter, r *http.Request) {
	var req ResetPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	if err := h.svc.ResetPassword(r.Context(), req); err != nil {
		writeAppError(w, r, err)
		return
	}

	respond.JSON(w, http.StatusOK, map[string]string{"message": "password updated successfully"})
}

// ChangePassword godoc
// @Summary      Change password (logged in)
// @Description  Changes the authenticated user's password. Re-verifies currentPassword via bcrypt, enforces the register password policy on newPassword, revokes all refresh tokens, and issues a fresh token pair to the caller (every other device is signed out; the caller stays signed in). Sets a new HttpOnly refresh cookie.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      ChangePasswordRequest  true  "Current + new password"
// @Success      200   {object}  AuthEnvelope   "New access token + rotated refresh cookie"
// @Failure      401   {object}  ErrorEnvelope  "UNAUTHORIZED (missing access token or wrong current password)"
// @Failure      400   {object}  ErrorEnvelope  "WEAK_PASSWORD"
// @Failure      422   {object}  ErrorEnvelope  "INVALID_INPUT (password mismatch)"
// @Failure      429   {object}  ErrorEnvelope  "RATE_LIMITED"
// @Router       /auth/change-password [post]
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	resp, err := h.svc.ChangePassword(r.Context(), userID, req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	h.setRefreshCookie(w, resp.RefreshToken, resp.CookieMaxAge)
	respond.JSON(w, http.StatusOK, resp)
}

// VerifyEmail godoc
// @Summary      Verify email address
// @Description  Confirms a user's email using the single-use token from the verification email.
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      VerifyEmailRequest  true  "Verification token"
// @Success      200   {object}  MessageEnvelope
// @Failure      401   {object}  ErrorEnvelope  "INVALID_TOKEN (invalid, expired, or already-used)"
// @Failure      422   {object}  ErrorEnvelope  "INVALID_INPUT"
// @Router       /auth/verify-email [post]
func (h *Handler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req VerifyEmailRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	if err := h.svc.VerifyEmail(r.Context(), req); err != nil {
		writeAppError(w, r, err)
		return
	}

	respond.JSON(w, http.StatusOK, map[string]string{"message": "email verified successfully"})
}

// ResendVerification godoc
// @Summary      Resend the verification email
// @Description  Re-sends the email-verification link. Always returns 200 (no user enumeration).
// @Tags         auth
// @Accept       json
// @Produce      json
// @Param        body  body      ResendVerificationRequest  true  "Account email"
// @Success      200   {object}  MessageEnvelope
// @Failure      429   {object}  ErrorEnvelope  "RATE_LIMITED"
// @Router       /auth/resend-verification [post]
func (h *Handler) ResendVerification(w http.ResponseWriter, r *http.Request) {
	var req ResendVerificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	// Always 200 — no enumeration.
	_ = h.svc.ResendVerification(r.Context(), req.Email)
	respond.JSON(w, http.StatusOK, map[string]string{"message": "if the email exists, a verification link has been sent"})
}

// GetProfile godoc
// @Summary      Get the current user
// @Description  Returns the authenticated user's profile.
// @Tags         users
// @Produce      json
// @Security     BearerAuth
// @Success      200  {object}  UserEnvelope
// @Failure      401  {object}  ErrorEnvelope  "UNAUTHORIZED"
// @Router       /users/profile [get]
func (h *Handler) GetProfile(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	view, err := h.svc.GetProfile(r.Context(), userID)
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	respond.JSON(w, http.StatusOK, view)
}

// UpdateMe godoc
// @Summary      Update the current user
// @Description  Updates profile fields. All fields are optional; omitted fields are left unchanged.
// @Tags         users
// @Accept       json
// @Produce      json
// @Security     BearerAuth
// @Param        body  body      UpdateMeRequest  true  "Fields to update"
// @Success      200   {object}  UserEnvelope
// @Failure      401   {object}  ErrorEnvelope  "UNAUTHORIZED"
// @Failure      422   {object}  ErrorEnvelope  "INVALID_INPUT"
// @Router       /users/me [patch]
func (h *Handler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	var req UpdateMeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond.Error(w, http.StatusBadRequest, apperror.ErrInvalidInput, "invalid request body")
		return
	}

	view, err := h.svc.UpdateMe(r.Context(), userID, req)
	if err != nil {
		writeAppError(w, r, err)
		return
	}

	respond.JSON(w, http.StatusOK, view)
}

// DeleteMe godoc
// @Summary      Delete the current account
// @Description  Soft-deletes the authenticated user (sets deleted_at) and revokes all refresh tokens.
// @Tags         users
// @Produce      json
// @Security     BearerAuth
// @Success      204  "Account deleted; all sessions revoked"
// @Failure      401  {object}  ErrorEnvelope  "UNAUTHORIZED"
// @Router       /users/me [delete]
func (h *Handler) DeleteMe(w http.ResponseWriter, r *http.Request) {
	userID := mw.UserIDFromCtx(r.Context())

	if err := h.svc.DeleteMe(r.Context(), userID); err != nil {
		writeAppError(w, r, err)
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
		writeAppError(w, r, err)
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

// Notification list/mark-read/delete moved to the notification domain (NIC-1515).
// Preferences (GET/PUT /notifications/preferences) land with NIC-1522.

// refreshCookieName returns the cookie name: "__Secure-refresh_token" in secure environments
// (staging/production) to block subdomain overwrite attacks, "refresh_token" in development.
func (h *Handler) refreshCookieName() string {
	if h.secureCookie {
		return "__Secure-refresh_token"
	}
	return "refresh_token"
}

// refreshCookieSameSite returns SameSite=None for cross-site deployments (frontend and API
// on different registrable domains) and SameSite=Strict otherwise. None is only ever returned
// when secureCookie is true (enforced in NewHandler), since browsers reject None without Secure.
func (h *Handler) refreshCookieSameSite() http.SameSite {
	if h.crossSite {
		return http.SameSiteNoneMode
	}
	return http.SameSiteStrictMode
}

// setRefreshCookie sets the HttpOnly refresh token cookie per SPEC §3.5.
// maxAge=0 produces a session cookie (no Max-Age header, expires on browser close).
// Secure is intentionally runtime-controlled (false in dev over HTTP, true in staging/production).
func (h *Handler) setRefreshCookie(w http.ResponseWriter, rawToken string, maxAge int) {
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure is intentionally dynamic: true in staging/production, false in development (HTTP)
		Name:     h.refreshCookieName(),
		Value:    rawToken,
		Path:     "/v1/auth",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: h.refreshCookieSameSite(),
	})
}

// clearRefreshCookie expires the refresh token cookie.
func (h *Handler) clearRefreshCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure is intentionally dynamic: true in staging/production, false in development (HTTP)
		Name:     h.refreshCookieName(),
		Value:    "",
		Path:     "/v1/auth",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.secureCookie,
		SameSite: h.refreshCookieSameSite(),
	})
}

// writeAppError converts an AppError to the correct HTTP response.
// Unexpected (non-AppError) errors are logged with the request ID before returning a generic 500.
func writeAppError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *apperror.AppError
	if errors.As(err, &ae) {
		respond.Error(w, ae.Status, ae.Code, ae.Message)
		return
	}
	log.Error().Err(err).Str("request_id", mw.GetRequestID(r.Context())).Msg("unexpected internal error")
	respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "an unexpected error occurred")
}
