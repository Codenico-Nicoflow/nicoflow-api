package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	anthropicclient "github.com/nicoflow/nicoflow-api/internal/anthropic"
	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/config"
	"github.com/nicoflow/nicoflow-api/internal/db"
	"github.com/nicoflow/nicoflow-api/internal/domain/ai"
	"github.com/nicoflow/nicoflow-api/internal/domain/area"
	"github.com/nicoflow/nicoflow-api/internal/domain/attachment"
	"github.com/nicoflow/nicoflow-api/internal/domain/auth"
	"github.com/nicoflow/nicoflow-api/internal/domain/billing"
	"github.com/nicoflow/nicoflow-api/internal/domain/bucket"
	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
	"github.com/nicoflow/nicoflow-api/internal/domain/project"
	"github.com/nicoflow/nicoflow-api/internal/domain/search"
	"github.com/nicoflow/nicoflow-api/internal/domain/task"
	"github.com/nicoflow/nicoflow-api/internal/handler"
	"github.com/nicoflow/nicoflow-api/internal/jobs"
	"github.com/nicoflow/nicoflow-api/internal/storage"
	"github.com/nicoflow/nicoflow-api/internal/ws"
	"github.com/nicoflow/nicoflow-api/pkg/pushutil"

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

	// WebSocket hub — in-process, single instance (no Redis in v1). The per-domain
	// adapters below inject it as each service's Broadcaster, so every successful
	// mutation fans out a full-payload event to the user's live connections
	// (NIC-1588 notifications, NIC-1629 domain events).
	wsHub := ws.NewHub()

	// Area domain.
	areaRepo := area.NewRepository(pool)
	areaSvc := area.NewService(areaRepo, ws.NewAreaBroadcaster(wsHub))

	// Project domain.
	projectRepo := project.NewRepository(pool)
	projectSvc := project.NewService(projectRepo, ws.NewProjectBroadcaster(wsHub))

	// Search — full-text across tasks, projects and areas.
	searchSvc := search.NewService(search.NewRepository(pool))

	// Notification domain. The ws adapter is the real-time Broadcaster: every
	// created notification fans out over WS (fire-and-forget). Built before
	// task/bucket so it can be injected as their real-time notifier. Web push
	// (NIC-1580): the sender is a no-op when VAPID is unconfigured, so this is safe
	// with empty keys locally.
	notificationRepo := notification.NewRepository(pool)
	pushSender, err := pushutil.New(cfg.VAPIDPublicKey, cfg.VAPIDPrivateKey, cfg.VAPIDSubject)
	if err != nil {
		log.Fatal().Err(err).Msg("invalid VAPID configuration")
	}
	notificationSvc := notification.NewService(notificationRepo, ws.NewNotificationBroadcaster(wsHub)).
		WithPushSender(notification.NewPushSender(notificationRepo, pushSender))

	// Task domain (incl. subtasks). notificationSvc drives task_completed +
	// project_completed real-time notifications (best-effort).
	taskRepo := task.NewRepository(pool)
	taskBroadcaster := ws.NewTaskBroadcaster(wsHub)
	taskSvc := task.NewService(taskRepo, notificationSvc, taskBroadcaster)
	subtaskSvc := task.NewSubtaskService(task.NewSubtaskRepository(pool), taskBroadcaster)

	// Bucket (inbox) — process turns an item into a task via the task service;
	// notificationSvc drives the Pro inbox_zero notification (best-effort).
	bucketSvc := bucket.NewService(bucket.NewRepository(pool), taskSvc, notificationSvc, ws.NewBucketBroadcaster(wsHub))

	// Object storage for file attachments (E-024). S3-compatible: MinIO locally,
	// Cloudflare R2 in staging/prod. Disabled (typed 503 at the request boundary)
	// when the STORAGE_* env vars are unset, so local/dev boots without a store.
	// The attachment domain (later stories) consumes it.
	storageClient, err := storage.New(ctx, cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to init storage client")
	}
	if storageClient.Enabled() {
		log.Info().Msg("file attachments: object storage enabled")
	} else {
		log.Warn().Msg("file attachments: object storage disabled (unset STORAGE_* env) — /attachments returns 503")
	}

	// Attachment domain (E-024 / NIC-1643). Owner ownership dispatches to the task
	// service via the adapter below; the GC sweep (NIC-1651) checks owner existence
	// system-wide via taskOwnerExistence over the task repo; storageClient is the
	// object-store port.
	attachmentSvc := attachment.NewService(
		attachment.NewRepository(pool),
		storageClient,
		taskOwnerVerifier{tasks: taskSvc},
		taskOwnerExistence{tasks: taskRepo},
		ws.NewAttachmentBroadcaster(wsHub),
	)

	// Wire the attachment cleaner back into the task service so deleting a task
	// best-effort reaps its attachments (NIC-1651). Post-construction because the
	// two services reference each other — the attachment service already depends on
	// the task service via the owner verifier, so this closes the loop acyclically
	// (concretes meet only here in wiring).
	taskSvc = taskSvc.WithCleaner(attachmentSvc)

	// AI assistant (E-026 / NIC-1681). Thin Anthropic streaming client behind the
	// ai.Client interface; unset ANTHROPIC_API_KEY ⇒ disabled client + a 503 kill
	// switch on /v1/ai/* (mirrors object storage). Model comes from config, never
	// hardcoded.
	aiClient := anthropicclient.New(cfg.AnthropicAPIKey)
	if aiClient.Enabled() {
		log.Info().Str("model", cfg.AIModel).Msg("ai assistant: enabled")
	} else {
		log.Warn().Msg("ai assistant: disabled (unset ANTHROPIC_API_KEY) — /v1/ai/* returns 503")
	}
	aiSvc := ai.NewService(ai.NewRepository(pool), aiClient, cfg.AIModel, ws.NewAIBroadcaster(wsHub))

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
		AI:           ai.NewHandler(aiSvc),
		Billing:      billing.NewHandler(nil),
		Search:       search.NewHandler(searchSvc),
		Attachment:   attachment.NewHandler(attachmentSvc),
		Notification: notification.NewHandler(notificationSvc),
		Jobs:         jobs.NewHandler(dueDateNotifier, overdueNotifier, dayStartNotifier, inboxNotifier, summaryNotifier, attachmentGCAdapter{svc: attachmentSvc}),
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
	// Close WS connections first: their read/write pumps run for the connection's
	// lifetime, so srv.Shutdown would otherwise block the full timeout waiting on
	// them. CloseAll sends each a clean close frame and lets the pumps exit, so the
	// HTTP drain below finishes fast.
	wsHub.CloseAll()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("graceful shutdown timed out")
	}
	log.Info().Msg("server shut down cleanly")
}

// taskOwnerVerifier adapts the task service to attachment.OwnerVerifier. It
// resolves ownership by attempting a user-scoped task lookup and normalizes any
// not-found (task-scoped or otherwise) into RESOURCE_NOT_FOUND so a foreign or
// missing owner never leaks its existence (AC6).
type taskOwnerVerifier struct {
	tasks task.Service
}

func (v taskOwnerVerifier) VerifyOwner(ctx context.Context, userID, ownerType, ownerID string) error {
	if ownerType != attachment.OwnerTypeTask {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "unknown owner type")
	}
	if _, err := v.tasks.Get(ctx, userID, ownerID); err != nil {
		if ae, ok := errors.AsType[*apperror.AppError](err); ok && ae.Status == http.StatusNotFound {
			return apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "resource not found")
		}
		return err
	}
	return nil
}

// taskOwnerExistence adapts the task repo to attachment.OwnerExistence for the GC
// sweep — a system-wide (no user scope) existence check. An unknown owner type is
// reported non-existent so its stale rows get reaped.
type taskOwnerExistence struct {
	tasks task.Repository
}

func (e taskOwnerExistence) OwnerExists(ctx context.Context, ownerType, ownerID string) (bool, error) {
	if ownerType != attachment.OwnerTypeTask {
		return false, nil
	}
	return e.tasks.ExistsByID(ctx, ownerID)
}

// attachmentGCAdapter adapts the attachment service to jobs.AttachmentGC,
// translating the domain summary into the jobs-facing one so neither package
// imports the other.
type attachmentGCAdapter struct {
	svc attachment.Service
}

func (a attachmentGCAdapter) RunGC(ctx context.Context) (jobs.GCSummary, error) {
	sum, err := a.svc.RunGC(ctx)
	if err != nil {
		return jobs.GCSummary{}, err
	}
	return jobs.GCSummary{ObjectsDeleted: sum.ObjectsDeleted, RowsDeleted: sum.RowsDeleted}, nil
}
