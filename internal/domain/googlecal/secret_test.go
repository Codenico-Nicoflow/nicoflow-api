package googlecal_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/googlecal"
)

const token = "1//0-super-secret-refresh-token"

// Every route a value can take out of the process — formatting, logging, JSON,
// a panic stack dump — funnels through one of these. A leak in any of them is a
// leak in all the paths that use it.
func TestSecret_NeverPrintsPlaintext(t *testing.T) {
	secret := googlecal.Secret(token)

	tests := []struct {
		name string
		got  func() string
	}{
		{name: "String()", got: secret.String},
		// %v and %q cover the fmt-verb path; a bare %s case is what staticcheck
		// flags as a String() call in disguise, and it proves nothing extra.
		{name: "%v", got: func() string { return fmt.Sprintf("%v", secret) }},
		{name: "%q", got: func() string { return fmt.Sprintf("%q", secret) }},
		{name: "%#v (GoStringer)", got: func() string { return fmt.Sprintf("%#v", secret) }},
		{name: "%+v on containing struct", got: func() string {
			return fmt.Sprintf("%+v", googlecal.Connection{RefreshToken: secret})
		}},
		{name: "%#v on containing struct", got: func() string {
			return fmt.Sprintf("%#v", googlecal.Connection{RefreshToken: secret})
		}},
		{name: "fmt.Errorf wrapping", got: func() string {
			return fmt.Errorf("connect failed: %v", secret).Error()
		}},
		{name: "json.Marshal", got: func() string {
			b, err := json.Marshal(secret)
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			return string(b)
		}},
		{name: "json.Marshal of containing struct", got: func() string {
			b, err := json.Marshal(googlecal.Connection{RefreshToken: secret})
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			return string(b)
		}},
		{name: "MarshalText", got: func() string {
			b, err := secret.MarshalText()
			if err != nil {
				t.Fatalf("MarshalText() error = %v", err)
			}
			return string(b)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := tt.got()
			if strings.Contains(out, token) {
				t.Errorf("output leaked the token: %s", out)
			}
			if !strings.Contains(out, "REDACTED") {
				t.Errorf("output = %s, want it to contain REDACTED", out)
			}
		})
	}
}

// Reveal is the single deliberate way out, so it must actually work — a Secret
// that could not be read back would be useless for calling Google.
func TestSecret_Reveal(t *testing.T) {
	if got := googlecal.Secret(token).Reveal(); got != token {
		t.Errorf("Reveal() = %q, want %q", got, token)
	}
}

func TestSecret_IsZero(t *testing.T) {
	tests := []struct {
		name   string
		secret googlecal.Secret
		want   bool
	}{
		{name: "empty", secret: "", want: true},
		{name: "populated", secret: googlecal.Secret(token), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.secret.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

// The view is what reaches the client. It must carry no token field at all —
// not a redacted one, not an encrypted one.
func TestConnection_ViewCarriesNoToken(t *testing.T) {
	now := time.Now()
	conn := googlecal.Connection{
		UserID:              "user-1",
		RefreshToken:        googlecal.Secret(token),
		GoogleAccountEmail:  "user@example.com",
		SelectedCalendarIDs: []string{"primary"},
		Scopes:              []string{"https://www.googleapis.com/auth/calendar.readonly"},
		ConnectedAt:         now,
	}

	b, err := json.Marshal(conn.View())
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	out := string(b)

	if strings.Contains(out, token) {
		t.Errorf("view leaked the token: %s", out)
	}
	for _, field := range []string{"refreshToken", "RefreshToken", "refresh_token", "REDACTED"} {
		if strings.Contains(out, field) {
			t.Errorf("view contains %q; it should have no token field at all: %s", field, out)
		}
	}
	if !strings.Contains(out, "user@example.com") {
		t.Errorf("view = %s, want it to carry the account email", out)
	}
}

// Nil slices would marshal as null; the client maps over these.
func TestConnection_ViewNormalisesNilSlices(t *testing.T) {
	view := googlecal.Connection{UserID: "user-1"}.View()

	if view.SelectedCalendarIDs == nil {
		t.Error("SelectedCalendarIDs = nil, want an empty slice")
	}
	if view.Scopes == nil {
		t.Error("Scopes = nil, want an empty slice")
	}
}
