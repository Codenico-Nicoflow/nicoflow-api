package handler_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/config"
	"github.com/nicoflow/nicoflow-api/internal/domain/auth"
	"github.com/nicoflow/nicoflow-api/internal/handler"
)

// nopAuthService satisfies auth.Service so a real auth.Handler can be wired into
// the router. Routing tests never reach these methods (auth middleware rejects
// the unauthenticated request first), so the bodies are inert.
type nopAuthService struct{}

func (nopAuthService) Register(context.Context, auth.RegisterRequest) (auth.AuthResponse, error) {
	return auth.AuthResponse{}, nil
}
func (nopAuthService) Login(context.Context, auth.LoginRequest) (auth.AuthResponse, error) {
	return auth.AuthResponse{}, nil
}
func (nopAuthService) Logout(context.Context, string, string) error { return nil }
func (nopAuthService) LogoutAll(context.Context, string) error      { return nil }
func (nopAuthService) RefreshToken(context.Context, string) (auth.AuthResponse, error) {
	return auth.AuthResponse{}, nil
}
func (nopAuthService) ForgotPassword(context.Context, string) error { return nil }
func (nopAuthService) ResetPassword(context.Context, auth.ResetPasswordRequest) error {
	return nil
}
func (nopAuthService) VerifyEmail(context.Context, auth.VerifyEmailRequest) error { return nil }
func (nopAuthService) ResendVerification(context.Context, string) error           { return nil }
func (nopAuthService) GetProfile(context.Context, string) (auth.UserView, error) {
	return auth.UserView{}, nil
}
func (nopAuthService) UpdateMe(context.Context, string, auth.UpdateMeRequest) (auth.UserView, error) {
	return auth.UserView{}, nil
}
func (nopAuthService) DeleteMe(context.Context, string) error { return nil }
func (nopAuthService) RegisterPushToken(context.Context, string, auth.RegisterPushTokenRequest) error {
	return nil
}

// TestRouter_ProtectedAuthRoutesAreReachable guards against the routing-shadow
// bug where /v1/auth/logout-all (and biometric/register) were registered on a
// separate /v1 Route block and never matched, because chi resolves /v1/auth/*
// into the public /v1/auth subrouter. An unauthenticated request must reach the
// auth middleware (401), NOT fall through to a 404.
func TestRouter_ProtectedAuthRoutesAreReachable(t *testing.T) {
	h := handler.Handlers{Auth: auth.NewHandler(nopAuthService{}, false)}
	router := handler.New(config.Config{JWTSecret: "test-secret-at-least-32-bytes-long!!"}, nil, h)

	cases := []struct {
		name, method, path string
	}{
		{"logout-all is routed (401, not 404)", http.MethodPost, "/v1/auth/logout-all"},
		{"biometric/register is routed (401, not 404)", http.MethodPost, "/v1/auth/biometric/register"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			if w.Code == http.StatusNotFound {
				t.Fatalf("%s %s returned 404 — route is shadowed/unreachable", tc.method, tc.path)
			}
			if w.Code != http.StatusUnauthorized {
				t.Errorf("%s %s status = %d, want 401 (rejected by auth middleware)", tc.method, tc.path, w.Code)
			}
		})
	}
}

// TestRouter_PublicAuthRoutesNotProtected confirms the public logout endpoint is
// reachable without a token (it authenticates off the refresh cookie).
func TestRouter_PublicAuthRoutesNotProtected(t *testing.T) {
	h := handler.Handlers{Auth: auth.NewHandler(nopAuthService{}, false)}
	router := handler.New(config.Config{JWTSecret: "test-secret-at-least-32-bytes-long!!"}, nil, h)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("public POST /v1/auth/logout status = %d, want 204", w.Code)
	}
}
