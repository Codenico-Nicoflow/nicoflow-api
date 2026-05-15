package middleware

import (
	"net/http"
	"strings"
)

// CORS returns a Chi-compatible middleware that sets CORS headers.
// allowedOrigins is a comma-separated list of allowed origin URLs.
func CORS(allowedOrigins string) func(http.Handler) http.Handler {
	origins := strings.Split(allowedOrigins, ",")
	set := make(map[string]struct{}, len(origins))
	for _, o := range origins {
		set[strings.TrimSpace(o)] = struct{}{}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if _, ok := set[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				// Required for browsers to send/receive cookies (e.g. HttpOnly refresh_token cookie).
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				// Prevents CDN/proxy from serving a cached response with a different Allow-Origin.
				w.Header().Add("Vary", "Origin")
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET,POST,PUT,PATCH,DELETE,OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization,Content-Type,X-Request-ID")
			if r.Method == http.MethodOptions {
				// Cache preflight for 24 h to reduce OPTIONS round-trips.
				w.Header().Set("Access-Control-Max-Age", "86400")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
