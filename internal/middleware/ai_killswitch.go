package middleware

import (
	"net/http"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// AIKillSwitch short-circuits every AI route with 503 AI_UNAVAILABLE when the
// Anthropic key is unset, before any handler logic runs. Mirrors the
// storage-disabled pattern: the feature is off, not a silent no-op.
func AIKillSwitch(enabled bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !enabled {
				respond.Error(w, http.StatusServiceUnavailable, apperror.ErrAIUnavailable, "ai assistant is not available")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
