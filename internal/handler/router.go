package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/config"
	"github.com/nicoflow/nicoflow-api/internal/domain/ai"
	"github.com/nicoflow/nicoflow-api/internal/domain/area"
	"github.com/nicoflow/nicoflow-api/internal/domain/auth"
	"github.com/nicoflow/nicoflow-api/internal/domain/billing"
	"github.com/nicoflow/nicoflow-api/internal/domain/bucket"
	"github.com/nicoflow/nicoflow-api/internal/domain/project"
	"github.com/nicoflow/nicoflow-api/internal/domain/task"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
)

// Handlers groups all domain handler pointers.
type Handlers struct {
	Auth    *auth.Handler
	Area    *area.Handler
	Project *project.Handler
	Task    *task.Handler
	Bucket  *bucket.Handler
	AI      *ai.Handler
	Billing *billing.Handler
}

// New builds and returns the fully-wired Chi router.
// Middleware order matches the Confluence §3.3 canonical chain.
func New(cfg config.Config, h Handlers) http.Handler {
	r := chi.NewRouter()

	// 1. Recover — panic → 500 JSON
	r.Use(mw.Recover)
	// 2. Request ID — inject/propagate X-Request-ID
	r.Use(mw.RequestID)
	// 3. Logger — zerolog structured request log
	r.Use(mw.Logger)
	// 4. CORS — Allow-Origin headers
	r.Use(mw.CORS(cfg.CORSOrigins))
	// 5. Rate limit by IP (global)
	r.Use(mw.RateLimitIP(100, 20))

	// ── Public routes ──────────────────────────────────────────────────────────
	r.Get("/v1/health", Health)

	// WS — JWT validated inside the handler once implemented (E-022)
	r.Get("/v1/ws", stub)

	// Billing webhook — HMAC verified inside handler, no JWT
	r.Post("/v1/billing/webhook", h.Billing.Webhook)

	// Auth — stricter IP rate limit
	r.Route("/v1/auth", func(r chi.Router) {
		r.Use(chimw.Maybe(mw.RateLimitIP(10, 5), func(r *http.Request) bool {
			return r.Method == http.MethodPost
		}))
		r.Post("/register", h.Auth.Register)
		r.Post("/login", h.Auth.Login)
		r.Post("/refresh-token", h.Auth.Refresh)
		r.Post("/forgot-password", h.Auth.ForgotPassword)
		r.Post("/reset-password", h.Auth.ResetPassword)
	})

	// ── Protected routes ───────────────────────────────────────────────────────
	r.Route("/v1", func(r chi.Router) {
		r.Use(mw.Auth(cfg.JWTSecret))
		r.Use(mw.RateLimitUser(1000, 100))

		// Auth — session management
		r.Post("/auth/logout", h.Auth.Logout)
		r.Post("/auth/logout-all", h.Auth.LogoutAll)

		// User profile & settings
		r.Get("/users/profile", h.Auth.GetProfile)
		r.Patch("/users/me", h.Auth.UpdateMe)
		r.Delete("/users/me", h.Auth.DeleteMe)
		r.Post("/users/push-token", h.Auth.RegisterPushToken)

		// Areas
		r.Get("/areas", h.Area.List)
		r.Post("/areas", h.Area.Create)
		r.Get("/areas/with-projects", h.Area.ListWithProjects)
		r.Get("/areas/{id}", h.Area.Get)
		r.Patch("/areas/{id}", h.Area.Update)
		r.Delete("/areas/{id}", h.Area.Delete)

		// Projects
		r.Get("/areas/{areaId}/projects", h.Project.ListByArea)
		r.Post("/areas/{areaId}/projects", h.Project.Create)
		r.Get("/projects/{id}", h.Project.Get)
		r.Patch("/projects/{id}", h.Project.Update)
		r.Delete("/projects/{id}", h.Project.Delete)

		// Tasks + Subtasks
		r.Get("/projects/{projectId}/tasks", h.Task.ListByProject)
		r.Post("/projects/{projectId}/tasks", h.Task.Create)
		r.Get("/tasks/{id}", h.Task.Get)
		r.Patch("/tasks/{id}", h.Task.Update)
		r.Delete("/tasks/{id}", h.Task.Delete)
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

		// Time Spread + Search
		r.Get("/time-spread", h.Task.TimeSpread)
		r.Get("/search", h.Task.Search)

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

		// Notifications
		r.Get("/notifications", h.Auth.ListNotifications)
		r.Patch("/notifications/read-all", h.Auth.MarkAllNotificationsRead)
		r.Patch("/notifications/{id}", h.Auth.MarkNotificationRead)
		r.Delete("/notifications/{id}", h.Auth.DeleteNotification)
		r.Get("/notifications/preferences", h.Auth.GetNotificationPreferences)
		r.Put("/notifications/preferences", h.Auth.UpdateNotificationPreferences)
	})

	return r
}

// stub is a placeholder for routes not yet assigned to a domain handler.
func stub(w http.ResponseWriter, _ *http.Request) {
	respond.Error(w, http.StatusNotImplemented, apperror.ErrInternalServerError, "not implemented")
}
