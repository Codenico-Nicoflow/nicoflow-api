package middleware

import "net/http"

// SecurityHeaders adds defence-in-depth HTTP security headers to every response.
//
// The CSP is locked down to `default-src 'none'` because the API only ever
// returns JSON — it has no scripts, styles, or assets of its own to permit, so
// the strictest policy is also the correct one. The one exception is the
// Swagger UI (an actual HTML page that loads JS/CSS and fetches its spec);
// that route relaxes the CSP via SwaggerCSP, applied after this middleware.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy", "default-src 'none'")
		next.ServeHTTP(w, r)
	})
}

// swaggerCSP is the relaxed policy the Swagger UI needs to render: it loads its
// bundled JS/CSS, applies inline styles/scripts (swagger-ui requires
// 'unsafe-inline'), shows inline data: images, and fetches doc.json same-origin.
// Everything is scoped to 'self' so the page still can't reach third-party
// origins. This route is non-production only (see the router), so the looser
// policy is never served in prod.
const swaggerCSP = "default-src 'none'; " +
	"script-src 'self' 'unsafe-inline'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"font-src 'self'; " +
	"connect-src 'self'"

// SwaggerCSP overrides the global default-src 'none' CSP with a Swagger-friendly
// policy for the routes it wraps. It must run after SecurityHeaders so its Set
// replaces the locked-down value rather than the other way round.
func SwaggerCSP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", swaggerCSP)
		next.ServeHTTP(w, r)
	})
}
