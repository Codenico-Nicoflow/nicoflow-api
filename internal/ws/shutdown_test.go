package ws

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nicoflow/nicoflow-api/pkg/jwtutil"
)

// Mirrors main.go's shutdown sequence: CloseAll first, then srv.Shutdown. A live
// WS connection must receive a normal-closure (1000) frame, and the whole
// sequence must finish well under the 15s shutdown timeout.
func TestShutdown_ClosesWSCleanlyAndFast(t *testing.T) {
	hub := NewHub()
	h := NewHandler(hub, testSecret, "")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(h.Upgrade), ReadHeaderTimeout: 10 * time.Second}
	go func() { _ = srv.Serve(ln) }()

	token, err := jwtutil.Issue("usr_1", "a@b.co", "free", testSecret, time.Minute)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	conn, _, err := websocket.DefaultDialer.Dial(wsURL("http://"+ln.Addr().String(), token), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	waitFor(t, func() bool { return hub.ClientCount("usr_1") == 1 }, "client registered")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	start := time.Now()
	hub.CloseAll()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Errorf("shutdown took %v, want well under the 15s timeout", elapsed)
	}

	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, _, readErr := conn.ReadMessage(); !websocket.IsCloseError(readErr, websocket.CloseNormalClosure) {
		t.Errorf("close code = %v, want 1000 CloseNormalClosure", readErr)
	}
}
