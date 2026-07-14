package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// InternalToken guards internal (machine-to-machine) endpoints with a shared
// secret sent as the X-Internal-Token header. It is the sole gate on those
// routes — they sit outside the JWT /v1 group.
//
//   - secret unset  ⇒ 503 (the endpoint is disabled, not misconfigured-open)
//   - header missing or wrong ⇒ 401
//
// The compare is constant-time to avoid leaking the secret via timing.
func InternalToken(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if secret == "" {
				respond.Error(w, http.StatusServiceUnavailable, apperror.ErrServiceUnavailable, "internal endpoint disabled")
				return
			}
			got := r.Header.Get("X-Internal-Token")
			if subtle.ConstantTimeCompare([]byte(got), []byte(secret)) != 1 {
				respond.Error(w, http.StatusUnauthorized, apperror.ErrUnauthorized, "missing or invalid internal token")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
