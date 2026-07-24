package ws

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nicoflow/nicoflow-api/pkg/jwtutil"
)

// dialN opens n real WS connections against the handler and waits until all are
// registered under userID. Returns the connections so the caller can close them.
func dialN(t *testing.T, srvURL, token, userID string, hub *Hub, n int) []*websocket.Conn {
	t.Helper()
	conns := make([]*websocket.Conn, 0, n)
	for range n {
		c, _, err := websocket.DefaultDialer.Dial(wsURL(srvURL, token), nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		conns = append(conns, c)
	}
	waitFor(t, func() bool {
		hub.mu.RLock()
		defer hub.mu.RUnlock()
		return len(hub.clients[userID]) == n
	}, "all clients registered")
	return conns
}

// A broadcast to a user with ~1000 live connections must reach every one of them
// without deadlocking. Guards the fan-out path under real concurrency + -race.
func TestHub_BroadcastToUser_FansOutAtScale(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping load test in -short")
	}
	const n = 1000
	hub := NewHub()
	h := NewHandler(hub, testSecret, "")
	srv := httptest.NewServer(http.HandlerFunc(h.Upgrade))
	defer srv.Close()

	token, err := jwtutil.Issue("usr_load", "a@b.co", "free", testSecret, time.Minute)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	conns := dialN(t, srv.URL, token, "usr_load", hub, n)
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()

	hub.BroadcastToUser("usr_load", Event{Event: EventNotificationCreated, Payload: map[string]string{"id": "n1"}, Timestamp: time.Now()})

	// Every connection must receive the single broadcast.
	for i, c := range conns {
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, _, readErr := c.ReadMessage(); readErr != nil {
			t.Fatalf("conn %d: read broadcast: %v", i, readErr)
		}
	}
}

// Opening and cleanly closing many connections must not leak goroutines: each
// connection's read+write pumps have to exit on disconnect. We compare the
// goroutine count before and after a churn of N connect/disconnect cycles.
func TestHub_NoGoroutineLeakOnConnectDisconnect(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping leak test in -short")
	}
	hub := NewHub()
	h := NewHandler(hub, testSecret, "")
	srv := httptest.NewServer(http.HandlerFunc(h.Upgrade))
	defer srv.Close()

	// Warm up one cycle so lazily-started runtime/server goroutines aren't counted.
	churn(t, srv.URL, hub, 20)
	waitForGoroutines(t)
	baseline := runtime.NumGoroutine()

	churn(t, srv.URL, hub, 200)
	waitForGoroutines(t)

	// The hub must hold no connections, and the goroutine count must return near
	// baseline (small slack for scheduler/GC noise). A per-connection pump leak
	// would grow this by ~2×200.
	hub.mu.RLock()
	remaining := len(hub.clients)
	hub.mu.RUnlock()
	if remaining != 0 {
		t.Errorf("hub still holds %d users after all disconnects", remaining)
	}
	if got := runtime.NumGoroutine(); got > baseline+20 {
		t.Errorf("goroutine count = %d, baseline %d — likely pump leak", got, baseline)
	}
}

// churn dials n connections (each a distinct user) then closes them, waiting for
// the hub to drain to empty so the pumps have run their deferred Unregister.
func churn(t *testing.T, srvURL string, hub *Hub, n int) {
	t.Helper()
	conns := make([]*websocket.Conn, 0, n)
	for i := range n {
		user := "usr_" + strconv.Itoa(i)
		token, err := jwtutil.Issue(user, "a@b.co", "free", testSecret, time.Minute)
		if err != nil {
			t.Fatalf("issue token: %v", err)
		}
		c, _, err := websocket.DefaultDialer.Dial(wsURL(srvURL, token), nil)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		conns = append(conns, c)
	}
	for _, c := range conns {
		_ = c.Close()
	}
	waitFor(t, func() bool {
		hub.mu.RLock()
		defer hub.mu.RUnlock()
		return len(hub.clients) == 0
	}, "hub drained to empty")
}

// waitForGoroutines gives closing pumps a moment to fully exit before sampling.
func waitForGoroutines(t *testing.T) {
	t.Helper()
	time.Sleep(200 * time.Millisecond)
	runtime.GC()
}
