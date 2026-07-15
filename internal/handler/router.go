package handler

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	httpSwagger "github.com/swaggo/http-swagger/v2"

	"github.com/nicoflow/nicoflow-api/internal/config"
	"github.com/nicoflow/nicoflow-api/internal/domain/ai"
	"github.com/nicoflow/nicoflow-api/internal/domain/area"
	"github.com/nicoflow/nicoflow-api/internal/domain/auth"
	"github.com/nicoflow/nicoflow-api/internal/domain/billing"
	"github.com/nicoflow/nicoflow-api/internal/domain/bucket"
	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
	"github.com/nicoflow/nicoflow-api/internal/domain/project"
	"github.com/nicoflow/nicoflow-api/internal/domain/search"
	"github.com/nicoflow/nicoflow-api/internal/domain/task"
	"github.com/nicoflow/nicoflow-api/internal/jobs"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/internal/ws"
)

// Handlers groups all domain handler pointers.
type Handlers struct {
	Auth         *auth.Handler
	Area         *area.Handler
	Project      *project.Handler
	Task         *task.Handler
	Bucket       *bucket.Handler
	AI           *ai.Handler
	Billing      *billing.Handler
	Search       *search.Handler
	Notification *notification.Handler
	Jobs         *jobs.Handler
	WS           *ws.Handler
}

// New builds and returns the fully-wired Chi router.
// Middleware order matches the Confluence §3.3 canonical chain.
func New(cfg config.Config, pool *pgxpool.Pool, h Handlers) http.Handler {
	r := chi.NewRouter()

	// 1. Recover — panic → 500 JSON
	r.Use(mw.Recover)
	// 2. Request ID — inject/propagate X-Request-ID
	r.Use(mw.RequestID)
	// 3. Logger — zerolog structured request log
	r.Use(mw.Logger)
	// 4. Security headers — HSTS, X-Frame-Options, CSP, etc.
	r.Use(mw.SecurityHeaders)
	// 5. CORS — Allow-Origin headers
	r.Use(mw.CORS(cfg.CORSOrigins))
	// 6. Rate limit by IP (global)
	trustedProxies := splitCSV(cfg.TrustedProxyCIDRs)
	r.Use(mw.RateLimitIP(100, 20, trustedProxies))

	// ── Public routes ──────────────────────────────────────────────────────────
	r.Get("/v1/health", Health(pool))

	// Swagger UI — auth API docs. Disabled in production to avoid exposing the surface.
	// SwaggerCSP relaxes the global default-src 'none' CSP so the UI's JS/CSS and its
	// doc.json fetch aren't blocked by the browser (curl/Postman ignore CSP, so this
	// only ever mattered for the in-browser UI).
	if cfg.AppEnv != "production" {
		r.With(mw.SwaggerCSP).Get("/v1/swagger/*", httpSwagger.Handler(httpSwagger.URL("/v1/swagger/doc.json")))
	}

	// WS — JWT validated inside the handler from the ?token= query param (E-022).
	// Public route: browsers can't set Authorization on the WS handshake, so the
	// auth middleware would reject it — the handler does its own JWT check.
	r.Get("/v1/ws", h.WS.Upgrade)

	// Billing webhook — HMAC verified inside handler, no JWT
	r.Post("/v1/billing/webhook", h.Billing.Webhook)

	// Internal jobs — machine-to-machine, guarded solely by the shared CRON_SECRET
	// (X-Internal-Token), outside the JWT /v1 group. Hit by the Render Cron Job.
	r.With(mw.InternalToken(cfg.CronSecret)).Post("/internal/jobs/due-notify", h.Jobs.DueNotify)

	// Auth — stricter per-endpoint IP rate limits
	r.Route("/v1/auth", func(r chi.Router) {
		r.With(mw.RateLimitIP(5, 5, trustedProxies)).Post("/register", h.Auth.Register)
		r.With(mw.RateLimitIP(10, 10, trustedProxies)).Post("/login", h.Auth.Login)
		r.Post("/refresh-token", h.Auth.Refresh)
		r.With(mw.RateLimitIP(3, 3, trustedProxies)).Post("/forgot-password", h.Auth.ForgotPassword)
		r.With(mw.RateLimitIP(5, 5, trustedProxies)).Post("/reset-password", h.Auth.ResetPassword)
		// Email verification (public; token-bearing or email-bearing).
		r.With(mw.RateLimitIP(10, 10, trustedProxies)).Post("/verify-email", h.Auth.VerifyEmail)
		r.With(mw.RateLimitIP(3, 3, trustedProxies)).Post("/resend-verification", h.Auth.ResendVerification)
		// Logout authenticates off the HttpOnly refresh cookie (Path=/v1/auth,
		// SameSite=Strict), not the access token — so an expired JWT can't trap
		// the user in a session they can't end. The handler is idempotent (no
		// cookie / already-gone token → 204), and SameSite=Strict blocks a
		// cross-site CSRF logout. logout-all stays protected (it revokes by the
		// userID claim). Matches SPEC §3 (logout is in the public auth block).
		r.Post("/logout", h.Auth.Logout)
		// Biometric stubs — FIDO2/WebAuthn in v2
		r.Post("/biometric/verify", h.Auth.BiometricVerify)

		// JWT-protected auth routes must live inside this same /v1/auth subrouter:
		// chi resolves /v1/auth/* into the subrouter mounted here, so registering
		// them on a separate /v1 Route block would be shadowed (404).
		r.Group(func(r chi.Router) {
			r.Use(mw.Auth(cfg.JWTSecret))
			r.Use(mw.RateLimitUser(1000, 100))
			// logout-all revokes by the userID claim, so it needs a live access token.
			r.Post("/logout-all", h.Auth.LogoutAll)
			r.Post("/biometric/register", h.Auth.BiometricRegister)
		})
	})

	// ── Protected routes ───────────────────────────────────────────────────────
	r.Route("/v1", func(r chi.Router) {
		r.Use(mw.Auth(cfg.JWTSecret))
		r.Use(mw.RateLimitUser(1000, 100))

		// User profile & settings
		r.Get("/users/profile", h.Auth.GetProfile)
		r.Patch("/users/me", h.Auth.UpdateMe)
		r.Delete("/users/me", h.Auth.DeleteMe)
		r.Post("/users/push-token", h.Auth.RegisterPushToken)

		// Areas — static routes before parameterised
		r.Get("/areas", h.Area.List)
		r.Post("/areas", h.Area.Create)
		r.Get("/areas/with-projects", h.Area.ListWithProjects)
		r.Patch("/areas/reorder", h.Area.Reorder)
		r.Get("/areas/{id}", h.Area.Get)
		r.Patch("/areas/{id}", h.Area.Update)
		r.Delete("/areas/{id}", h.Area.Delete)

		// Projects — static routes before parameterised
		r.Get("/projects", h.Project.List)
		r.Patch("/projects/reorder", h.Project.Reorder)
		r.Get("/projects/{id}", h.Project.Get)
		r.Patch("/projects/{id}", h.Project.Update)
		r.Delete("/projects/{id}", h.Project.Delete)
		r.Get("/areas/{areaId}/projects", h.Project.ListByArea)
		r.Post("/areas/{areaId}/projects", h.Project.Create)

		// Tasks + Subtasks
		r.Get("/projects/{projectId}/tasks", h.Task.ListByProject)
		r.Post("/projects/{projectId}/tasks", h.Task.Create)
		r.Get("/tasks/{id}", h.Task.Get)
		r.Patch("/tasks/{id}", h.Task.Update)
		r.Delete("/tasks/{id}", h.Task.Delete)
		r.Patch("/tasks/{id}/status", h.Task.SetStatus)
		r.Patch("/tasks/{id}/schedule", h.Task.Schedule)
		r.Patch("/tasks/{id}/reorder", h.Task.ReorderOne)
		r.Get("/tasks/{taskId}/subtasks", h.Task.ListSubtasks)
		r.Post("/tasks/{taskId}/subtasks", h.Task.CreateSubtask)
		r.Patch("/tasks/{taskId}/subtasks/{subtaskId}", h.Task.UpdateSubtask)
		r.Delete("/tasks/{taskId}/subtasks/{subtaskId}", h.Task.DeleteSubtask)

		// Attachments (task-scoped create/list + attachment-scoped download/delete)
		r.Get("/tasks/{taskId}/attachments", h.Task.ListAttachments)
		r.Post("/tasks/{taskId}/attachments", h.Task.CreateAttachment)
		r.Get("/attachments/{id}/download", h.Task.DownloadAttachment)
		r.Delete("/attachments/{id}", h.Task.DeleteAttachment)

		// Bucket (inbox)
		r.Get("/bucket", h.Bucket.List)
		r.Get("/bucket/{id}", h.Bucket.Get)
		r.Post("/bucket", h.Bucket.Create)
		r.Patch("/bucket/{id}", h.Bucket.Update)
		r.Delete("/bucket/{id}", h.Bucket.Delete)
		r.Post("/bucket/{id}/process", h.Bucket.Process)

		// Focus — "what can I do right now?" (deterministic ranked list)
		r.Get("/focus", h.Task.Focus)

		// Time Spread + Search
		r.Get("/time-spread", h.Task.TimeSpread)
		r.Get("/search", h.Search.Search)

		// AI sessions + messages
		r.Get("/ai/sessions", h.AI.ListSessions)
		r.Post("/ai/sessions", h.AI.CreateSession)
		r.Get("/ai/sessions/{id}", h.AI.GetSession)
		r.Delete("/ai/sessions/{id}", h.AI.DeleteSession)
		r.Get("/ai/sessions/{id}/messages", h.AI.ListMessages)
		r.Post("/ai/sessions/{id}/messages", h.AI.SendMessage)

		// NLP smart scheduling (Pro only — PlanEnforcer added in E-028)
		r.Post("/nlp/parse", h.AI.ParseNLP)

		// Billing
		r.Get("/billing/plan", h.Billing.GetPlan)
		r.Get("/billing/checkout-url", h.Billing.CheckoutURL)
		r.Get("/billing/portal-url", h.Billing.PortalURL)

		// Notifications — static routes before parameterised.
		r.Get("/notifications", h.Notification.List)
		r.Get("/notifications/preferences", h.Notification.GetPreferences)
		r.Put("/notifications/preferences", h.Notification.UpdatePreferences)
		r.Get("/notifications/unread-count", h.Notification.UnreadCount)
		r.Patch("/notifications/read-all", h.Notification.MarkAllRead)
		r.Patch("/notifications/{id}/read", h.Notification.MarkRead)
		r.Delete("/notifications/{id}", h.Notification.Delete)
	})

	return r
}

// splitCSV splits a comma-separated string into a trimmed, non-empty slice.
func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
