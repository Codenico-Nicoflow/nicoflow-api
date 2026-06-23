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
	"github.com/nicoflow/nicoflow-api/internal/domain/project"
	"github.com/nicoflow/nicoflow-api/internal/domain/task"
	"github.com/nicoflow/nicoflow-api/internal/handler"

	// Generated Swagger docs (make swagger). Imported for the side-effect of
	// registering the spec; the /v1/swagger UI route reads it.
	_ "github.com/nicoflow/nicoflow-api/docs"
)

// @title           Nicoflow API
// @version         1.0
// @description     REST API for the Nicoflow GTD task-management platform. This spec currently documents the authentication & user-management surface.
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

	// Apply pending DB migrations on boot so a deploy can never run against a
	// schema older than the code expects (the single-instance Render setup has no
	// separate migration step). Idempotent; opt out with SKIP_MIGRATIONS=true.
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

	handlers := handler.Handlers{
		Auth:    auth.NewHandler(authSvc, cookieCfg),
		Area:    area.NewHandler(areaSvc),
		Project: project.NewHandler(projectSvc),
		Task:    task.NewHandler(nil),
		Bucket:  bucket.NewHandler(nil),
		AI:      ai.NewHandler(nil),
		Billing: billing.NewHandler(nil),
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
	log.Info().Msg("server shut down cleanly")
}
