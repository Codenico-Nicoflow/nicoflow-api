package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/config"
	"github.com/nicoflow/nicoflow-api/internal/db"
	"github.com/nicoflow/nicoflow-api/internal/domain/ai"
	"github.com/nicoflow/nicoflow-api/internal/domain/area"
	"github.com/nicoflow/nicoflow-api/internal/domain/auth"
	"github.com/nicoflow/nicoflow-api/internal/domain/billing"
	"github.com/nicoflow/nicoflow-api/internal/domain/bucket"
	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
	"github.com/nicoflow/nicoflow-api/internal/domain/project"
	"github.com/nicoflow/nicoflow-api/internal/domain/search"
	"github.com/nicoflow/nicoflow-api/internal/domain/task"
	"github.com/nicoflow/nicoflow-api/internal/handler"
	"github.com/nicoflow/nicoflow-api/internal/jobs"
	"github.com/nicoflow/nicoflow-api/internal/ws"

	// Generated Swagger docs (make swagger). Imported for the side-effect of
	// registering the spec; the /v1/swagger UI route reads it.
	_ "github.com/nicoflow/nicoflow-api/docs"
)

// @title           Nicoflow API
// @version         1.0
// @description     REST API for the Nicoflow GTD task-management platform. Covers authentication & user management, Areas, Projects, Tasks (CRUD, quick actions, filter/sort/search), Subtasks, Focus and Time-Spread.
// @BasePath        /v1
// @schemes         http https
// @securityDefinitions.apikey  BearerAuth
// @in                          header
// @name                        Authorization
// @description                 JWT access token as "Bearer <token>".
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

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Apply pending migrations on boot so code never runs ahead of the schema.
	if !cfg.SkipMigrations {
		if err := db.Migrate(cfg.DatabaseURL); err != nil {
			log.Fatal().Err(err).Msg("failed to apply database migrations")
		}
	}

	pool, err := db.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer pool.Close()

	// Auth domain — fully wired.
	authRepo := auth.NewRepository(pool)
	authSvc := auth.NewService(authRepo, cfg)
	auth.StartTokenGC(ctx, pool)
	secureCookie := cfg.AppEnv == "production" || cfg.AppEnv == "staging"
	cookieCfg := auth.HandlerConfig{SecureCookie: secureCookie, CrossSite: cfg.CookieCrossSite}

	// Area domain.
	areaRepo := area.NewRepository(pool)
	areaSvc := area.NewService(areaRepo)

	// Project domain.
	projectRepo := project.NewRepository(pool)
	projectSvc := project.NewService(projectRepo)

	// Search — full-text across tasks, projects and areas.
	searchSvc := search.NewService(search.NewRepository(pool))

	// WebSocket hub — in-process, single instance (no Redis in v1). The notification
	// Broadcaster is wired to it in a follow-up (NIC-1588); for now the hub serves
	// live connections and the broadcaster stays nil.
	wsHub := ws.NewHub()

	// Notification domain. Broadcaster is nil until the hub is injected (NIC-1588).
	// Built before task/bucket so it can be injected as their real-time notifier.
	notificationSvc := notification.NewService(notification.NewRepository(pool), nil)

	// Task domain (incl. subtasks). notificationSvc drives task_completed +
	// project_completed real-time notifications (best-effort).
	taskRepo := task.NewRepository(pool)
	taskSvc := task.NewService(taskRepo, notificationSvc)
	subtaskSvc := task.NewSubtaskService(task.NewSubtaskRepository(pool))

	// Bucket (inbox) — process turns an item into a task via the task service;
	// notificationSvc drives the Pro inbox_zero notification (best-effort).
	bucketSvc := bucket.NewService(bucket.NewRepository(pool), taskSvc, notificationSvc)

	// Sweep jobs — hourly, invoked by Render Cron Jobs via /internal/jobs/*.
	jobsRepo := jobs.NewRepository(pool)
	dueDateNotifier := jobs.NewDueDateNotifier(jobsRepo, notificationSvc, cfg.SMTPDsn)
	overdueNotifier := jobs.NewOverdueNotifier(jobsRepo, notificationSvc)
	dayStartNotifier := jobs.NewDayStartNotifier(jobsRepo, notificationSvc)
	inboxNotifier := jobs.NewInboxNotifier(jobsRepo, notificationSvc)
	summaryNotifier := jobs.NewSummaryNotifier(jobsRepo, notificationSvc)

	handlers := handler.Handlers{
		Auth:         auth.NewHandler(authSvc, cookieCfg),
		Area:         area.NewHandler(areaSvc),
		Project:      project.NewHandler(projectSvc),
		Task:         task.NewHandler(taskSvc, subtaskSvc),
		Bucket:       bucket.NewHandler(bucketSvc),
		AI:           ai.NewHandler(nil),
		Billing:      billing.NewHandler(nil),
		Search:       search.NewHandler(searchSvc),
		Notification: notification.NewHandler(notificationSvc),
		Jobs:         jobs.NewHandler(dueDateNotifier, overdueNotifier, dayStartNotifier, inboxNotifier, summaryNotifier),
		WS:           ws.NewHandler(wsHub, cfg.JWTSecret, cfg.CORSOrigins),
	}

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler.New(cfg, pool, handlers),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Info().Str("port", cfg.Port).Str("env", cfg.AppEnv).Msg("server starting")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("server stopped unexpectedly")
		}
	}()

	<-ctx.Done()
	log.Info().Msg("shutdown signal received — draining connections")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("graceful shutdown timed out")
	}
	wsHub.CloseAll()
	log.Info().Msg("server shut down cleanly")
}
