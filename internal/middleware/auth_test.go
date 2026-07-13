package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/jwtutil"
)

// Same throwaway literal the domain integration tests use, so the secret
// scanner treats it as a known test fixture rather than a leak.
const testSecret = "integration-test-secret-32-bytes!!"

// errorCode reads the {data,error:{code}} envelope and returns error.code.
func errorCode(t *testing.T, body []byte) string {
	t.Helper()
	var env struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if env.Error == nil {
		return ""
	}
	return env.Error.Code
}

func TestAuth(t *testing.T) {
	valid, err := jwtutil.Issue("user-1", "a@b.com", "pro", testSecret, time.Hour)
	if err != nil {
		t.Fatalf("issue valid token: %v", err)
	}
	expired, err := jwtutil.Issue("user-1", "a@b.com", "pro", testSecret, -time.Minute)
	if err != nil {
		t.Fatalf("issue expired token: %v", err)
	}
	// Derived (not a new literal) so Auth(testSecret) rejects it as a bad signature.
	wrongSecret, err := jwtutil.Issue("user-1", "a@b.com", "pro", testSecret+"-rotated", time.Hour)
	if err != nil {
		t.Fatalf("issue wrong-secret token: %v", err)
	}

	tests := []struct {
		name       string
		header     string
		wantStatus int
		wantCode   string // empty ⇒ expect the request to pass through
	}{
		{"valid token passes", "Bearer " + valid, http.StatusOK, ""},
		{"missing header", "", http.StatusUnauthorized, apperror.ErrUnauthorized},
		{"malformed prefix", "Token " + valid, http.StatusUnauthorized, apperror.ErrUnauthorized},
		{"expired token", "Bearer " + expired, http.StatusUnauthorized, apperror.ErrInvalidToken},
		{"tampered signature", "Bearer " + valid + "tamper", http.StatusUnauthorized, apperror.ErrInvalidToken},
		{"signed with wrong secret", "Bearer " + wrongSecret, http.StatusUnauthorized, apperror.ErrInvalidToken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotUserID, gotPlan, gotEmail string
			nextCalled := false
			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				nextCalled = true
				gotUserID = UserIDFromCtx(r.Context())
				gotPlan = PlanFromCtx(r.Context())
				gotEmail = EmailFromCtx(r.Context())
			})

			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.header != "" {
				r.Header.Set("Authorization", tt.header)
			}
			w := httptest.NewRecorder()

			Auth(testSecret)(next).ServeHTTP(w, r)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tt.wantStatus)
			}

			if tt.wantCode == "" {
				if !nextCalled {
					t.Fatal("next handler was not called for a valid token")
				}
				if gotUserID != "user-1" || gotPlan != "pro" || gotEmail != "a@b.com" {
					t.Errorf("ctx claims = (%q,%q,%q), want (user-1,pro,a@b.com)", gotUserID, gotPlan, gotEmail)
				}
				return
			}

			if nextCalled {
				t.Error("next handler ran despite rejected auth")
			}
			if got := errorCode(t, w.Body.Bytes()); got != tt.wantCode {
				t.Errorf("error code = %q, want %q", got, tt.wantCode)
			}
		})
	}
}
