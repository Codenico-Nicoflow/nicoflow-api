package middleware

import (
	"net/http"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Recover catches panics, logs them with zerolog, and writes a 500 JSON response.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Error().
					Interface("panic", rec).
					Str("method", r.Method).
					Str("path", r.URL.Path).
					Str("request_id", GetRequestID(r.Context())).
					Msg("panic recovered")
				respond.Error(w, http.StatusInternalServerError, apperror.ErrInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
