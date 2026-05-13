package main

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-playground/validator/v10"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/config"
	"github.com/nicoflow/nicoflow-api/internal/db"
	"github.com/nicoflow/nicoflow-api/internal/handler"
	ginmiddleware "github.com/nicoflow/nicoflow-api/internal/middleware"
	pgrepo "github.com/nicoflow/nicoflow-api/internal/repository/postgres"
	"github.com/nicoflow/nicoflow-api/internal/service"
	"github.com/nicoflow/nicoflow-api/internal/validations"
	"github.com/nicoflow/nicoflow-api/internal/ws"
)

func initValidators() {
	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		if err := v.RegisterValidation("strong_password", validations.PasswordValidator); err != nil {
			log.Fatal().Err(err).Msg("failed to register strong_password validator")
		}
	}
}

func main() {
	if os.Getenv("APP_ENV") != "production" {
		_ = godotenv.Load()
	}

	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	if os.Getenv("APP_ENV") != "production" {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	} else {
		log.Logger = zerolog.New(os.Stdout).With().Timestamp().Logger()
	}

	cfg := config.Load()

	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)

	initValidators()

	ctx := context.Background()
	pool, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer pool.Close()

	// Repositories
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

	// Services
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

	// Handlers
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

	// Gin engine for legacy routes (migrated to Chi story-by-story)
	if cfg.AppEnv == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	ginEngine := gin.New()

	// Request-ID and structured logging are handled by the outer Chi middleware
	// chain. Gin only needs CORS and recovery for the routes it owns.
	ginEngine.Use(
		ginmiddleware.CORS(cfg.CORSOrigins),
		gin.Recovery(),
	)

	v1 := ginEngine.Group("/v1")

	// Public
	auth := v1.Group("/auth", ginmiddleware.RateLimitIP(10, 10))
	{
		auth.POST("/register", authH.Register)
		auth.POST("/login", authH.Login)
		auth.POST("/refresh", authH.Refresh)
	}
	v1.POST("/billing/webhook", billingH.Webhook)

	// Authenticated
	protected := v1.Group("", ginmiddleware.Auth(cfg.JWTSecret), ginmiddleware.RateLimitUser(100, 100))
	{
		protected.POST("/auth/logout", authH.Logout)
		protected.POST("/auth/logout-all", authH.LogoutAll)
		protected.DELETE("/users/me", userH.DeleteMe)

		protected.GET("/ws", wsH.Upgrade)

		protected.GET("/areas", areaH.List)
		protected.POST("/areas", areaH.Create)
		protected.PATCH("/areas/:id", areaH.Update)
		protected.DELETE("/areas/:id", areaH.Delete)

		protected.GET("/projects", projectH.List)
		protected.POST("/projects", projectH.Create)
		protected.PATCH("/projects/:id", projectH.Update)
		protected.DELETE("/projects/:id", projectH.Delete)

		tasks := protected.Group("/tasks")
		{
			tasks.GET("", taskH.List)
			tasks.POST("", taskH.Create)
			tasks.PATCH("/:taskId", taskH.Update)
			tasks.DELETE("/:taskId", taskH.Delete)

			subtasks := tasks.Group("/:taskId/subtasks")
			{
				subtasks.GET("", subtaskH.List)
				subtasks.POST("", subtaskH.Create)
				subtasks.PATCH("/:subtaskId", subtaskH.Update)
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

	// Chi top-level router
	r := chi.NewRouter()
	r.Use(ginmiddleware.RequestID)
	r.Use(ginmiddleware.Logger)
	r.Use(middleware.Recoverer)

	// Health — Chi-native, net/http handler
	r.Get("/v1/health", handler.Health)

	// All other routes delegated to Gin engine
	r.Mount("/", ginEngine)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Info().Str("port", cfg.Port).Str("env", cfg.AppEnv).Msg("server starting")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal().Err(err).Msg("server stopped")
	}
}
