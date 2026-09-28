package googlecal_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/domain/googlecal"
)

type redirectService struct {
	callbackPath string
	callbackErr  error
	cancelPath   string
	cancelErr    error
}

func (s redirectService) Connect(context.Context, string, string) (string, error) {
	return "", errors.New("unexpected Connect call")
}
func (s redirectService) Callback(context.Context, string, string) (string, error) {
	return s.callbackPath, s.callbackErr
}
func (s redirectService) Cancel(context.Context, string) (string, error) {
	return s.cancelPath, s.cancelErr
}
func (redirectService) Get(context.Context, string) (googlecal.ConnectionView, error) {
	return googlecal.ConnectionView{}, errors.New("unexpected Get call")
}
func (redirectService) Disconnect(context.Context, string) error {
	return errors.New("unexpected Disconnect call")
}
func (redirectService) RevokeForUser(context.Context, string) {}

func TestHandler_CallbackReturnsToNativeAppWithStatus(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		service    redirectService
		wantStatus string
	}{
		{
			name:       "connected",
			query:      "state=valid&code=auth-code",
			service:    redirectService{callbackPath: "nicoflow:///settings"},
			wantStatus: "connected",
		},
		{
			name:       "denied",
			query:      "state=valid&error=access_denied",
			service:    redirectService{cancelPath: "nicoflow:///settings"},
			wantStatus: "denied",
		},
		{
			name:       "exchange failed",
			query:      "state=valid&code=auth-code",
			service:    redirectService{callbackPath: "nicoflow:///settings", callbackErr: errors.New("exchange failed")},
			wantStatus: "failed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := googlecal.NewHandler(test.service, "https://app.example.com")
			req := httptest.NewRequest(http.MethodGet, "/v1/calendar/google/callback?"+test.query, nil)
			response := httptest.NewRecorder()

			handler.Callback(response, req)

			if response.Code != http.StatusFound {
				t.Fatalf("Callback status = %d, want %d", response.Code, http.StatusFound)
			}
			if got, want := response.Header().Get("Location"), "nicoflow:///settings?google="+test.wantStatus; got != want {
				t.Fatalf("Location = %q, want %q", got, want)
			}
		})
	}
}
