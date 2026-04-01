// @title						Nicoflow API
// @version					1.0
// @description				REST API for the Nicoflow productivity platform
// @host						localhost:8080
// @BasePath					/v1
// @securityDefinitions.apikey	BearerAuth
// @in							header
// @name						Authorization
// @description				Enter: Bearer <token>
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "nicoflow-api/docs"
	"nicoflow-api/internal/config"
	"nicoflow-api/internal/db"
	"nicoflow-api/internal/handler"
	"nicoflow-api/internal/middleware"
	pgrepo "nicoflow-api/internal/repository/postgres"
	"nicoflow-api/internal/service"
	"nicoflow-api/internal/validations"
	"nicoflow-api/internal/ws"
)

func InitValidators() {
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		err := v.RegisterValidation("strong_password", validations.PasswordValidator)
		if err != nil {
			slog.Error("failed to register strong_password validator", "err", err)
		}
	}
}

func main() {
	// Load .env in non-production environments (no-op if file doesn't exist)
	if os.Getenv("APP_ENV") != "production" {
		_ = godotenv.Load()
	}

	cfg := config.Load()

	// --- Initialize Validators --------------------------------------------------
	InitValidators()
	// --- Database ───────────────────────────────────────────────────────────────
	ctx := context.Background()
	pool, err := db.New(ctx, cfg.DBUrl)
	if err != nil {
		slog.Error("failed to connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	// ── Repositories ────────────────────────────────────────────────────────────
	userRepo := pgrepo.NewUserRepo(pool)
	refreshTokenRepo := pgrepo.NewRefreshTokenRepo(pool)
	areaRepo := pgrepo.NewAreaRepo(pool)
	projectRepo := pgrepo.NewProjectRepo(pool)
	taskRepo := pgrepo.NewTaskRepo(pool)
	subtaskRepo := pgrepo.NewSubtaskRepo(pool)
	planRepo := pgrepo.NewUserPlanRepo(pool)
	webhookEventRepo := pgrepo.NewWebhookEventRepo(pool)
	aiSessionRepo := pgrepo.NewAISessionRepo(pool)
	aiMessageRepo := pgrepo.NewAIMessageRepo(pool)
	aiUsageRepo := pgrepo.NewAIUsageMonthlyRepo(pool)

	// ── Services ────────────────────────────────────────────────────────────────
	authSvc := service.NewAuthService(userRepo, refreshTokenRepo, planRepo, cfg.JWTSecret)
	userSvc := service.NewUserService(userRepo, refreshTokenRepo)
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
	nlpSvc := service.NewNLPService(planRepo)

	// ── Handlers ────────────────────────────────────────────────────────────────
	authH := handler.NewAuthHandler(authSvc)
	userH := handler.NewUserHandler(userSvc)
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
	nlpH := handler.NewNLPHandler(nlpSvc)

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

	// Swagger UI — only in non-production environments
	if cfg.AppEnv != "production" {
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	}

	v1 := r.Group("/v1")

	// Public
	v1.GET("/health", handler.Health)
	auth := v1.Group("/auth", middleware.RateLimitIP(10, 10))
	{
		auth.POST("/register", authH.Register)
		auth.POST("/login", authH.Login)
		auth.POST("/refresh", authH.Refresh)
	}
	v1.POST("/billing/webhook", billingH.Webhook)

	// Authenticated
	protected := v1.Group("", middleware.Auth(cfg.JWTSecret), middleware.RateLimitUser(100, 100))
	{
		protected.POST("/auth/logout", authH.Logout)
		protected.POST("/auth/logout-all", authH.LogoutAll)
		protected.DELETE("/users/me", userH.DeleteMe)

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

		protected.POST("/nlp", nlpH.Parse)
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
