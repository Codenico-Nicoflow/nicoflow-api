package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

func TestAIKillSwitch(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		wantStatus  int
		wantCode    string
		wantNextHit bool
	}{
		{name: "disabled returns 503 before handler", enabled: false, wantStatus: http.StatusServiceUnavailable, wantCode: apperror.ErrAIUnavailable},
		{name: "enabled passes through to handler", enabled: true, wantStatus: http.StatusOK, wantNextHit: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			nextHit := false
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				nextHit = true
				w.WriteHeader(http.StatusOK)
			})

			rec := httptest.NewRecorder()
			AIKillSwitch(tc.enabled)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/ai/sessions", nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if nextHit != tc.wantNextHit {
				t.Fatalf("next hit = %v, want %v", nextHit, tc.wantNextHit)
			}
			if tc.wantCode != "" {
				var body struct {
					Error *struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if body.Error == nil || body.Error.Code != tc.wantCode {
					t.Fatalf("error code = %+v, want %q", body.Error, tc.wantCode)
				}
			}
		})
	}
}
