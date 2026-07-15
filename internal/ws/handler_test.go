package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nicoflow/nicoflow-api/pkg/jwtutil"
)

// Same throwaway literal the other tests use, so the secret scanner treats it
// as a known test fixture rather than a leak.
const testSecret = "integration-test-secret-32-bytes!!"

// wsURL turns an httptest http:// URL into a ws:// URL with a token query.
func wsURL(serverURL, token string) string {
	u := "ws" + strings.TrimPrefix(serverURL, "http")
	if token != "" {
		return u + "?token=" + token
	}
	return u
}

func TestHandler_Upgrade_ValidTokenRegistersAndReceives(t *testing.T) {
	hub := NewHub()
	h := NewHandler(hub, testSecret, "")
	srv := httptest.NewServer(http.HandlerFunc(h.Upgrade))
	defer srv.Close()

	token, err := jwtutil.Issue("usr_1", "a@b.co", "free", testSecret, time.Minute)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, token), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// The client should be registered under its userID.
	waitFor(t, func() bool {
		hub.mu.RLock()
		defer hub.mu.RUnlock()
		return len(hub.clients["usr_1"]) == 1
	}, "client registered")

	// A broadcast to that user must arrive on the socket.
	hub.BroadcastToUser("usr_1", Event{Event: EventNotificationCreated, Payload: map[string]string{"id": "n1"}, Timestamp: time.Now()})
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read broadcast: %v", err)
	}
	if !strings.Contains(string(msg), string(EventNotificationCreated)) {
		t.Errorf("payload = %q, want it to contain %q", msg, EventNotificationCreated)
	}
}

func TestHandler_Upgrade_InvalidTokenClosesWith1008(t *testing.T) {
	hub := NewHub()
	h := NewHandler(hub, testSecret, "")
	srv := httptest.NewServer(http.HandlerFunc(h.Upgrade))
	defer srv.Close()

	tests := []struct {
		name  string
		token string
	}{
		{"missing token", ""},
		{"garbage token", "not-a-jwt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv.URL, tt.token), nil)
			if err != nil {
				// Some stacks surface the close during the handshake — acceptable.
				return
			}
			defer conn.Close()

			_ = conn.SetReadDeadline(time.Now().Add(time.Second))
			_, _, readErr := conn.ReadMessage()
			if readErr == nil {
				t.Fatal("expected the connection to be closed, but read succeeded")
			}
			if !websocket.IsCloseError(readErr, websocket.ClosePolicyViolation) {
				t.Errorf("close code = %v, want 1008 ClosePolicyViolation", readErr)
			}

			// No client should have been registered.
			hub.mu.RLock()
			n := len(hub.clients)
			hub.mu.RUnlock()
			if n != 0 {
				t.Errorf("registered clients = %d, want 0", n)
			}
		})
	}
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for: %s", what)
}
