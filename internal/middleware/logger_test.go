package middleware

import (
	"bufio"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// hijackableWriter is an httptest.ResponseRecorder that also supports hijacking,
// standing in for the real net/http connection writer.
type hijackableWriter struct {
	*httptest.ResponseRecorder
	hijacked bool
}

func (h *hijackableWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h.hijacked = true
	return nil, nil, nil
}

// TestStatusRecorder_Hijack_PassesThrough guards the WebSocket upgrade path: the
// Logger's statusRecorder must expose http.Hijacker so gorilla's Upgrade can take
// over the TCP connection. Without this, every /v1/ws upgrade 500s in the full
// middleware chain (regression fix).
func TestStatusRecorder_Hijack_PassesThrough(t *testing.T) {
	base := &hijackableWriter{ResponseRecorder: httptest.NewRecorder()}
	rec := &statusRecorder{ResponseWriter: base, status: http.StatusOK}

	hj, ok := any(rec).(http.Hijacker)
	if !ok {
		t.Fatal("statusRecorder does not implement http.Hijacker — WS upgrade will fail")
	}
	if _, _, err := hj.Hijack(); err != nil {
		t.Fatalf("Hijack returned error: %v", err)
	}
	if !base.hijacked {
		t.Error("Hijack did not pass through to the underlying writer")
	}
}

// TestStatusRecorder_Hijack_UnsupportedUnderlying returns an error (not a panic)
// when the underlying writer can't hijack.
func TestStatusRecorder_Hijack_UnsupportedUnderlying(t *testing.T) {
	rec := &statusRecorder{ResponseWriter: httptest.NewRecorder(), status: http.StatusOK}
	_, _, err := rec.Hijack()
	if err == nil {
		t.Fatal("expected an error when the underlying writer is not a Hijacker")
	}
}
