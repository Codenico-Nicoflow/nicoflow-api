package middleware

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

// statusRecorder wraps http.ResponseWriter to capture the status code written
// by downstream handlers.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Hijack passes through to the underlying ResponseWriter so a WebSocket upgrade
// (which needs to take over the TCP connection) works through this wrapper. Without
// it, the wrapper hides the http.Hijacker interface and gorilla's Upgrade fails.
func (r *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := r.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, errors.New("underlying ResponseWriter does not support hijacking")
	}
	return hj.Hijack()
}

// Logger is a Chi-compatible middleware that emits one structured zerolog line
// per request:
//
//	{"level":"info","request_id":"…","method":"GET","path":"/health","status":200,"latency_ms":3}
//
// It must be registered after the RequestID middleware so the request_id field
// is populated.
func Logger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rec, r)

		latencyMS := time.Since(start).Milliseconds()
		requestID := GetRequestID(r.Context())

		log.WithLevel(zerolog.InfoLevel).
			Str("request_id", requestID).
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", rec.status).
			Int64("latency_ms", latencyMS).
			Msg("")
	})
}
