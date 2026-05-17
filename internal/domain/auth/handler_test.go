package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/auth"
)

// mockService implements auth.Service for handler-level tests.
type mockService struct {
	loginFn func(ctx context.Context, req auth.LoginRequest) (auth.AuthResponse, error)
}

func (m *mockService) Register(_ context.Context, _ auth.RegisterRequest) (auth.AuthResponse, error) {
	return auth.AuthResponse{CookieMaxAge: int(testCfg().RefreshTokenExpiry.Seconds())}, nil
}
func (m *mockService) Login(ctx context.Context, req auth.LoginRequest) (auth.AuthResponse, error) {
	return m.loginFn(ctx, req)
}
func (m *mockService) Logout(_ context.Context, _, _ string) error { return nil }
func (m *mockService) LogoutAll(_ context.Context, _ string) error { return nil }
func (m *mockService) RefreshToken(_ context.Context, _ string) (auth.AuthResponse, error) {
	return auth.AuthResponse{CookieMaxAge: int(testCfg().RefreshTokenExpiry.Seconds())}, nil
}
func (m *mockService) ForgotPassword(_ context.Context, _ string) error { return nil }
func (m *mockService) ResetPassword(_ context.Context, _ auth.ResetPasswordRequest) error {
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

			h := auth.NewHandler(svc, false)
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
