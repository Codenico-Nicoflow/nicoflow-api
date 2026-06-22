package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/auth"
)

// mockService implements auth.Service for handler-level tests. Each method
// delegates to an optional func field; nil fields fall back to a benign default
// so a test only wires the behaviour it cares about.
type mockService struct {
	loginFn    func(ctx context.Context, req auth.LoginRequest) (auth.AuthResponse, error)
	registerFn func(ctx context.Context, req auth.RegisterRequest) (auth.AuthResponse, error)
	refreshFn  func(ctx context.Context, raw string) (auth.AuthResponse, error)
	resetFn    func(ctx context.Context, req auth.ResetPasswordRequest) error
	verifyFn   func(ctx context.Context, req auth.VerifyEmailRequest) error
}

func (m *mockService) Register(ctx context.Context, req auth.RegisterRequest) (auth.AuthResponse, error) {
	if m.registerFn != nil {
		return m.registerFn(ctx, req)
	}
	return auth.AuthResponse{CookieMaxAge: int(testCfg().RefreshTokenExpiry.Seconds())}, nil
}
func (m *mockService) Login(ctx context.Context, req auth.LoginRequest) (auth.AuthResponse, error) {
	return m.loginFn(ctx, req)
}
func (m *mockService) Logout(_ context.Context, _, _ string) error { return nil }
func (m *mockService) LogoutAll(_ context.Context, _ string) error { return nil }
func (m *mockService) RefreshToken(ctx context.Context, raw string) (auth.AuthResponse, error) {
	if m.refreshFn != nil {
		return m.refreshFn(ctx, raw)
	}
	return auth.AuthResponse{CookieMaxAge: int(testCfg().RefreshTokenExpiry.Seconds())}, nil
}
func (m *mockService) ForgotPassword(_ context.Context, _ string) error { return nil }
func (m *mockService) ResetPassword(ctx context.Context, req auth.ResetPasswordRequest) error {
	if m.resetFn != nil {
		return m.resetFn(ctx, req)
	}
	return nil
}
func (m *mockService) VerifyEmail(ctx context.Context, req auth.VerifyEmailRequest) error {
	if m.verifyFn != nil {
		return m.verifyFn(ctx, req)
	}
	return nil
}
func (m *mockService) ResendVerification(_ context.Context, _ string) error {
	return nil
}
func (m *mockService) GetProfile(_ context.Context, _ string) (auth.UserView, error) {
	return auth.UserView{}, nil
}
func (m *mockService) UpdateMe(_ context.Context, _ string, _ auth.UpdateMeRequest) (auth.UserView, error) {
	return auth.UserView{}, nil
}
func (m *mockService) DeleteMe(_ context.Context, _ string) error { return nil }
func (m *mockService) RegisterPushToken(_ context.Context, _ string, _ auth.RegisterPushTokenRequest) error {
	return nil
}

func loginBody(t *testing.T, remember bool) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(auth.LoginRequest{
		Email:    "user@example.com",
		Password: "Secret123",
		Remember: remember,
	})
	if err != nil {
		t.Fatalf("marshal login body: %v", err)
	}
	return bytes.NewBuffer(b)
}

func TestHandler_Login_RememberMe_Cookie(t *testing.T) {
	cfg := testCfg()

	tests := []struct {
		name             string
		remember         bool
		svcMaxAge        int
		wantCookieMaxAge int
	}{
		{
			name:             "remember=true sets persistent cookie with full Max-Age",
			remember:         true,
			svcMaxAge:        int(cfg.RefreshTokenExpiry.Seconds()), // 604800
			wantCookieMaxAge: int(cfg.RefreshTokenExpiry.Seconds()),
		},
		{
			name:             "remember=false sets session cookie with Max-Age=0",
			remember:         false,
			svcMaxAge:        int((24 * time.Hour).Seconds()), // 86400
			wantCookieMaxAge: int((24 * time.Hour).Seconds()),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &mockService{
				loginFn: func(_ context.Context, _ auth.LoginRequest) (auth.AuthResponse, error) {
					return auth.AuthResponse{
						Token:        "jwt.token.here",
						RefreshToken: "rawrefreshtoken",
						User:         auth.UserView{ID: "usr_1", Email: "user@example.com"},
						CookieMaxAge: tt.svcMaxAge,
					}, nil
				},
			}

			h := auth.NewHandler(svc, auth.HandlerConfig{})
			r := httptest.NewRequest(http.MethodPost, "/v1/auth/login", loginBody(t, tt.remember))
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			h.Login(w, r)

			res := w.Result()
			if res.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", res.StatusCode)
			}

			var cookie *http.Cookie
			for _, c := range res.Cookies() {
				if c.Name == "refresh_token" {
					cookie = c
					break
				}
			}
			if cookie == nil {
				t.Fatal("refresh_token cookie not set")
			}
			if cookie.MaxAge != tt.wantCookieMaxAge {
				t.Errorf("cookie Max-Age = %d, want %d", cookie.MaxAge, tt.wantCookieMaxAge)
			}
			if !cookie.HttpOnly {
				t.Error("cookie must be HttpOnly")
			}
			if cookie.SameSite != http.SameSiteStrictMode {
				t.Error("cookie must be SameSite=Strict")
			}
		})
	}
}

// TestHandler_Login_CrossSiteCookie asserts the refresh cookie is configured for a
// cross-site deployment: secure environment + CrossSite=true yields the secure cookie
// name with SameSite=None and Secure (so the browser sends it on cross-origin refresh).
func TestHandler_Login_CrossSiteCookie(t *testing.T) {
	svc := &mockService{
		loginFn: func(_ context.Context, _ auth.LoginRequest) (auth.AuthResponse, error) {
			return auth.AuthResponse{
				Token:        "jwt.token.here",
				RefreshToken: "rawrefreshtoken",
				User:         auth.UserView{ID: "usr_1", Email: "user@example.com"},
				CookieMaxAge: int(testCfg().RefreshTokenExpiry.Seconds()),
			}, nil
		},
	}

	h := auth.NewHandler(svc, auth.HandlerConfig{SecureCookie: true, CrossSite: true})
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/login", loginBody(t, true))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Login(w, r)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}

	var cookie *http.Cookie
	for _, c := range res.Cookies() {
		if c.Name == "__Secure-refresh_token" {
			cookie = c
			break
		}
	}
	if cookie == nil {
		t.Fatal("__Secure-refresh_token cookie not set in secure cross-site mode")
	}
	if cookie.SameSite != http.SameSiteNoneMode {
		t.Errorf("cookie SameSite = %v, want None for cross-site", cookie.SameSite)
	}
	if !cookie.Secure {
		t.Error("cross-site cookie must be Secure (SameSite=None requires it)")
	}
}

// TestHandler_Login_CrossSiteIgnoredWithoutSecure asserts CrossSite is ignored over plain
// HTTP (SecureCookie=false): SameSite=None without Secure is rejected by browsers, so the
// handler must fall back to Strict rather than emit an unusable cookie.
func TestHandler_Login_CrossSiteIgnoredWithoutSecure(t *testing.T) {
	svc := &mockService{
		loginFn: func(_ context.Context, _ auth.LoginRequest) (auth.AuthResponse, error) {
			return auth.AuthResponse{
				Token:        "jwt.token.here",
				RefreshToken: "rawrefreshtoken",
				User:         auth.UserView{ID: "usr_1", Email: "user@example.com"},
				CookieMaxAge: int(testCfg().RefreshTokenExpiry.Seconds()),
			}, nil
		},
	}

	h := auth.NewHandler(svc, auth.HandlerConfig{SecureCookie: false, CrossSite: true})
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/login", loginBody(t, true))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.Login(w, r)

	cookie := refreshCookie(w.Result())
	if cookie == nil {
		t.Fatal("refresh_token cookie not set")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("cookie SameSite = %v, want Strict (None ignored without Secure)", cookie.SameSite)
	}
}

// envelope mirrors the respond package's wire shape for decoding in tests.
type envelope struct {
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeEnvelope(t *testing.T, res *http.Response) envelope {
	t.Helper()
	var env envelope
	if err := json.NewDecoder(res.Body).Decode(&env); err != nil {
		t.Fatalf("decode envelope: %v", err)
	}
	return env
}

func refreshCookie(res *http.Response) *http.Cookie {
	for _, c := range res.Cookies() {
		if c.Name == "refresh_token" {
			return c
		}
	}
	return nil
}

func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}
	return bytes.NewBuffer(b)
}

// TestHandler_Register_NoSession asserts register returns 201 with a user but no
// token and crucially does NOT set a refresh cookie (no auto-login).
func TestHandler_Register_NoSession(t *testing.T) {
	svc := &mockService{
		registerFn: func(_ context.Context, _ auth.RegisterRequest) (auth.AuthResponse, error) {
			return auth.AuthResponse{User: auth.UserView{ID: "usr_1", Email: "user@example.com"}}, nil
		},
	}
	h := auth.NewHandler(svc, auth.HandlerConfig{})
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/register", jsonBody(t, auth.RegisterRequest{
		Email: "user@example.com", Username: "user", Password: "Secret123",
	}))
	w := httptest.NewRecorder()

	h.Register(w, r)

	res := w.Result()
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", res.StatusCode)
	}
	if c := refreshCookie(res); c != nil {
		t.Errorf("register must not set a refresh cookie, got %q", c.Value)
	}
	env := decodeEnvelope(t, res)
	if env.Error != nil {
		t.Fatalf("error = %+v, want nil", env.Error)
	}
	var data auth.AuthResponse
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if data.Token != "" {
		t.Errorf("token = %q, want empty (no auto-login)", data.Token)
	}
	if data.User.Email != "user@example.com" {
		t.Errorf("user.email = %q, want user@example.com", data.User.Email)
	}
}

// TestHandler_Login_Unverified asserts a 403 EMAIL_NOT_VERIFIED is surfaced
// as a structured envelope with no cookie set.
func TestHandler_Login_Unverified(t *testing.T) {
	svc := &mockService{
		loginFn: func(_ context.Context, _ auth.LoginRequest) (auth.AuthResponse, error) {
			return auth.AuthResponse{}, apperror.New(http.StatusForbidden, apperror.ErrEmailNotVerified, "email not verified")
		},
	}
	h := auth.NewHandler(svc, auth.HandlerConfig{})
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/login", loginBody(t, false))
	w := httptest.NewRecorder()

	h.Login(w, r)

	res := w.Result()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", res.StatusCode)
	}
	if c := refreshCookie(res); c != nil {
		t.Error("rejected login must not set a refresh cookie")
	}
	env := decodeEnvelope(t, res)
	if env.Error == nil || env.Error.Code != apperror.ErrEmailNotVerified {
		t.Fatalf("error code = %+v, want EMAIL_NOT_VERIFIED", env.Error)
	}
}

// TestHandler_Refresh_NoToken asserts a refresh with neither cookie nor body
// returns 401 INVALID_TOKEN.
func TestHandler_Refresh_NoToken(t *testing.T) {
	svc := &mockService{
		refreshFn: func(_ context.Context, raw string) (auth.AuthResponse, error) {
			if raw == "" {
				return auth.AuthResponse{}, apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "invalid refresh token")
			}
			return auth.AuthResponse{Token: "new", RefreshToken: "rotated"}, nil
		},
	}
	h := auth.NewHandler(svc, auth.HandlerConfig{})
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/refresh-token", bytes.NewBufferString("{}"))
	w := httptest.NewRecorder()

	h.Refresh(w, r)

	res := w.Result()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.StatusCode)
	}
	env := decodeEnvelope(t, res)
	if env.Error == nil || env.Error.Code != apperror.ErrInvalidToken {
		t.Fatalf("error code = %+v, want INVALID_TOKEN", env.Error)
	}
}

// TestHandler_Logout_ClearsCookie asserts logout returns 204 and expires the
// refresh cookie even with no cookie on the request (idempotent).
func TestHandler_Logout_ClearsCookie(t *testing.T) {
	h := auth.NewHandler(&mockService{}, auth.HandlerConfig{})
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	w := httptest.NewRecorder()

	h.Logout(w, r)

	res := w.Result()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", res.StatusCode)
	}
	c := refreshCookie(res)
	if c == nil {
		t.Fatal("logout must emit a cookie-clearing Set-Cookie")
	}
	if c.MaxAge >= 0 {
		t.Errorf("cookie Max-Age = %d, want negative (expired)", c.MaxAge)
	}
}

// TestHandler_LogoutAll_ClearsCookie asserts logout-all returns 204 and clears
// the refresh cookie.
func TestHandler_LogoutAll_ClearsCookie(t *testing.T) {
	h := auth.NewHandler(&mockService{}, auth.HandlerConfig{})
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/logout-all", nil)
	w := httptest.NewRecorder()

	h.LogoutAll(w, r)

	res := w.Result()
	if res.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", res.StatusCode)
	}
	if c := refreshCookie(res); c == nil || c.MaxAge >= 0 {
		t.Error("logout-all must expire the refresh cookie")
	}
}

// TestHandler_ResetPassword_InvalidToken asserts a service INVALID_TOKEN error
// is surfaced as 401 with the right code.
func TestHandler_ResetPassword_InvalidToken(t *testing.T) {
	svc := &mockService{
		resetFn: func(_ context.Context, _ auth.ResetPasswordRequest) error {
			return apperror.New(http.StatusUnauthorized, apperror.ErrInvalidToken, "invalid reset token")
		},
	}
	h := auth.NewHandler(svc, auth.HandlerConfig{})
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/reset-password", jsonBody(t, auth.ResetPasswordRequest{
		Token: "bad", NewPassword: "Secret123", ConfirmPassword: "Secret123",
	}))
	w := httptest.NewRecorder()

	h.ResetPassword(w, r)

	res := w.Result()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", res.StatusCode)
	}
	env := decodeEnvelope(t, res)
	if env.Error == nil || env.Error.Code != apperror.ErrInvalidToken {
		t.Fatalf("error code = %+v, want INVALID_TOKEN", env.Error)
	}
}

// TestHandler_VerifyEmail_Success asserts a happy verify returns 200 with a
// message payload and no error.
func TestHandler_VerifyEmail_Success(t *testing.T) {
	h := auth.NewHandler(&mockService{}, auth.HandlerConfig{})
	r := httptest.NewRequest(http.MethodPost, "/v1/auth/verify-email", jsonBody(t, auth.VerifyEmailRequest{Token: "good"}))
	w := httptest.NewRecorder()

	h.VerifyEmail(w, r)

	res := w.Result()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}
	env := decodeEnvelope(t, res)
	if env.Error != nil {
		t.Fatalf("error = %+v, want nil", env.Error)
	}
}
