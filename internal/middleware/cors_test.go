package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCORS_AllowedOriginExposesRateLimitHeaders(t *testing.T) {
	h := CORS("http://localhost:5173")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Errorf("Allow-Origin = %q, want the request origin", got)
	}
	expose := rec.Header().Get("Access-Control-Expose-Headers")
	for _, want := range []string{"Retry-After", "X-RateLimit-Reset", "X-RateLimit-Remaining"} {
		if !strings.Contains(expose, want) {
			t.Errorf("Expose-Headers %q missing %q", expose, want)
		}
	}
}

func TestCORS_DisallowedOriginGetsNoAllowOrigin(t *testing.T) {
	h := CORS("http://localhost:5173")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty for a disallowed origin", got)
	}
}
