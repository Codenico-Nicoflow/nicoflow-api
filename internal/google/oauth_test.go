package google

import (
	"net/url"
	"slices"
	"strings"
	"testing"
)

func TestClient_AuthCodeURL(t *testing.T) {
	c := New("client-id", "client-secret", "https://api.example.com/v1/calendar/google/callback")

	raw := c.AuthCodeURL("state-token")

	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse auth url: %v", err)
	}
	q := u.Query()

	// The email scope is what makes the post-exchange userinfo call work; without
	// it Google returns 401 there and every connect fails at the last step.
	scopes := strings.Fields(q.Get("scope"))
	for _, want := range []string{CalendarReadonlyScope, EmailScope} {
		if !slices.Contains(scopes, want) {
			t.Errorf("scope %q missing from consent URL, got %v", want, scopes)
		}
	}

	// access_type=offline is what yields a refresh token at all; prompt=consent
	// forces one on re-authorisation. A connection without a refresh token looks
	// successful and behaves as broken.
	if got := q.Get("access_type"); got != "offline" {
		t.Errorf("access_type = %q, want offline", got)
	}
	if got := q.Get("prompt"); got != "consent" {
		t.Errorf("prompt = %q, want consent", got)
	}
	if got := q.Get("state"); got != "state-token" {
		t.Errorf("state = %q, want state-token", got)
	}
	if got := q.Get("redirect_uri"); got != "https://api.example.com/v1/calendar/google/callback" {
		t.Errorf("redirect_uri = %q", got)
	}
}
