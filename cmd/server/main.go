package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"nicoflow-api/internal/config"
	"nicoflow-api/internal/handler"
	"nicoflow-api/internal/middleware"
	"nicoflow-api/internal/service"
	"nicoflow-api/internal/ws"
)

func main() {
	cfg := config.Load()

	// ── Repositories ────────────────────────────────────────────────────────────
	var (
		userRepo         = nilUserRepo{}
		refreshTokenRepo = nilRefreshTokenRepo{}
		areaRepo         = nilAreaRepo{}
		projectRepo      = nilProjectRepo{}
		taskRepo         = nilTaskRepo{}
		subtaskRepo      = nilSubtaskRepo{}
		planRepo         = nilUserPlanRepo{}
		webhookEventRepo = nilWebhookEventRepo{}
		aiSessionRepo    = nilAISessionRepo{}
		aiMessageRepo    = nilAIMessageRepo{}
		aiUsageRepo      = nilAIUsageMonthlyRepo{}
	)

	// ── Services ────────────────────────────────────────────────────────────────
	authSvc := service.NewAuthService(userRepo, refreshTokenRepo, cfg.JWTSecret)
	areaSvc := service.NewAreaService(areaRepo, planRepo)
	projectSvc := service.NewProjectService(projectRepo, planRepo)
	taskSvc := service.NewTaskService(taskRepo)
	subtaskSvc := service.NewSubtaskService(subtaskRepo)
	inboxSvc := service.NewInboxService(taskRepo)
	timeSpreadSvc := service.NewTimeSpreadService(taskRepo)
	userPlanSvc := service.NewUserPlanService(planRepo)
	aiSessionSvc := service.NewAISessionService(aiSessionRepo, planRepo)
	aiMessageSvc := service.NewAIMessageService(aiMessageRepo, aiSessionRepo, aiUsageRepo, planRepo)
	billingSvc := service.NewBillingService(webhookEventRepo, planRepo)
	attachSvc := service.NewAttachmentService(cfg.S3Bucket)
	hub := ws.NewHub()
	wsSvc := service.NewWSService(hub)

	// ── Handlers ────────────────────────────────────────────────────────────────
	authH := handler.NewAuthHandler(authSvc)
	areaH := handler.NewAreaHandler(areaSvc)
	projectH := handler.NewProjectHandler(projectSvc)
	taskH := handler.NewTaskHandler(taskSvc)
	subtaskH := handler.NewSubtaskHandler(subtaskSvc)
	inboxH := handler.NewInboxHandler(inboxSvc)
	timeSpreadH := handler.NewTimeSpreadHandler(timeSpreadSvc)
	userPlanH := handler.NewUserPlanHandler(userPlanSvc)
	aiSessionH := handler.NewAISessionHandler(aiSessionSvc)
	aiMessageH := handler.NewAIMessageHandler(aiMessageSvc)
	billingH := handler.NewBillingHandler(billingSvc)
	attachH := handler.NewAttachmentHandler(attachSvc)
	wsH := handler.NewWSHandler(wsSvc)

	// ── Router ──────────────────────────────────────────────────────────────────
	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.New()

	var logger gin.HandlerFunc
	if cfg.AppEnv == "production" {
		logger = middleware.Logger()
	} else {
		logger = gin.Logger()
	}

	r.Use(
		middleware.RequestID(),
		logger,
		middleware.CORS(cfg.CORSOrigins),
		gin.Recovery(),
	)

	v1 := r.Group("/v1")

	// Public
	v1.GET("/health", handler.Health)
	auth := v1.Group("/auth")
	{
		auth.POST("/register", authH.Register)
		auth.POST("/login", authH.Login)
		auth.POST("/refresh", authH.Refresh)
		auth.POST("/logout", authH.Logout)
	}
	v1.POST("/billing/webhook", billingH.Webhook)

	// Authenticated
	protected := v1.Group("", middleware.Auth(cfg.JWTSecret))
	{
		protected.GET("/ws", wsH.Upgrade)

		protected.GET("/areas", areaH.List)
		protected.POST("/areas", areaH.Create)
		protected.PUT("/areas/:id", areaH.Update)
		protected.DELETE("/areas/:id", areaH.Delete)

		protected.GET("/projects", projectH.List)
		protected.POST("/projects", projectH.Create)
		protected.PUT("/projects/:id", projectH.Update)
		protected.DELETE("/projects/:id", projectH.Delete)

		tasks := protected.Group("/tasks")
		{
			tasks.GET("", taskH.List)
			tasks.POST("", taskH.Create)
			tasks.PUT("/:taskId", taskH.Update)
			tasks.DELETE("/:taskId", taskH.Delete)

			subtasks := tasks.Group("/:taskId/subtasks")
			{
				subtasks.GET("", subtaskH.List)
				subtasks.POST("", subtaskH.Create)
				subtasks.PUT("/:subtaskId", subtaskH.Update)
				subtasks.DELETE("/:subtaskId", subtaskH.Delete)
			}
		}

		protected.POST("/inbox/capture", inboxH.Capture)
		protected.GET("/inbox", inboxH.List)

		protected.GET("/time-spread", timeSpreadH.Get)

		aiSessions := protected.Group("/ai/sessions")
		{
			aiSessions.GET("", aiSessionH.List)
			aiSessions.POST("", aiSessionH.Create)
			aiSessions.DELETE("/:id", aiSessionH.Delete)

			messages := aiSessions.Group("/:sessionId/messages")
			{
				messages.GET("", aiMessageH.ListBySession)
				messages.POST("", aiMessageH.Send)
			}
		}

		protected.GET("/user/plan", userPlanH.Get)

		protected.GET("/billing/checkout-url", billingH.CheckoutURL)
		protected.GET("/billing/portal-url", billingH.PortalURL)

		protected.POST("/attachments/upload-url", attachH.UploadURL)
		protected.POST("/attachments/download-url", attachH.DownloadURL)
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
	slog.Info("server starting", "port", cfg.Port, "env", cfg.AppEnv)
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}
