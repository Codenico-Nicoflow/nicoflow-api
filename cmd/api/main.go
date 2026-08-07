package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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
	"github.com/nicoflow/nicoflow-api/internal/domain/focus"
	"github.com/nicoflow/nicoflow-api/internal/domain/googlecal"
	"github.com/nicoflow/nicoflow-api/internal/domain/habit"
	"github.com/nicoflow/nicoflow-api/internal/domain/note"
	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
	"github.com/nicoflow/nicoflow-api/internal/domain/project"
	"github.com/nicoflow/nicoflow-api/internal/domain/recurrence"
	"github.com/nicoflow/nicoflow-api/internal/domain/search"
	"github.com/nicoflow/nicoflow-api/internal/domain/task"
	"github.com/nicoflow/nicoflow-api/internal/google"
	"github.com/nicoflow/nicoflow-api/internal/handler"
	"github.com/nicoflow/nicoflow-api/internal/jobs"
	"github.com/nicoflow/nicoflow-api/internal/storage"
	"github.com/nicoflow/nicoflow-api/internal/ws"
	"github.com/nicoflow/nicoflow-api/pkg/cryptoutil"
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

	// Note repository — built here rather than with the note service below because
	// the attachment owner seams need it: a note is a valid attachment owner
	// (E-053), and the GC sweep resolves note owners through the same repo.
	noteRepo := note.NewRepository(pool)

	// Attachment domain (E-024 / NIC-1643). Owner ownership dispatches to the task
	// service or the note repo via the adapter below; the GC sweep (NIC-1651) checks owner existence
	// system-wide via taskOwnerExistence over the task repo; storageClient is the
	// object-store port.
	attachmentSvc := attachment.NewService(
		attachment.NewRepository(pool),
		storageClient,
		ownerVerifier{tasks: taskSvc, notes: noteRepo},
		ownerExistence{tasks: taskRepo, notes: noteRepo},
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
	aiRepo := ai.NewRepository(pool)
	aiSvc := ai.NewService(aiRepo, aiClient, cfg.AIModel, ws.NewAIBroadcaster(wsHub))

	// Task recurrence (E-050 / NIC-1772). Rule CRUD; creating a rule materializes
	// instance #1 in the same transaction. FREE on every plan for reads; the
	// 3-rule cap on free is enforced in the service.
	recurrenceRepo := recurrence.NewRepository(pool)
	recurrenceSvc := recurrence.NewService(recurrenceRepo, ws.NewRecurrenceBroadcaster(wsHub))

	// The materializer (E-050 / NIC-1773) drives both triggers: the hourly cron
	// sweep via run-all, and the synchronous successor on completion. It enforces
	// the same per-project active-task ceiling as the task service.
	recurrenceMaterializer := recurrence.NewMaterializer(
		recurrenceRepo, ws.NewRecurrenceBroadcaster(wsHub), task.FreePlanTaskLimit,
	)
	// Wire the synchronous trigger back into the task service so completing a
	// recurring occurrence spawns its successor in the same request. Post-
	// construction because the two reference each other.
	taskSvc = taskSvc.WithMaterializer(recurrenceMaterializer)

	// Focus timer (E-049 / NIC-1710). Server-authoritative segments with a
	// one-open-per-user invariant; FREE on every plan. taskRepo satisfies the
	// narrow TaskOwnershipChecker seam, so focus never imports the task package.
	focusRepo := focus.NewRepository(pool)
	focusSvc := focus.NewService(focusRepo, taskRepo, ws.NewFocusBroadcaster(wsHub))
	// Wire focus totals back into the task service so Focus + GetTask responses
	// carry totalFocusSeconds (NIC-1712); the reverse seam keeps task ↛ focus.
	taskSvc = taskSvc.WithFocusTotals(focusRepo)

	// Project notes (E-053 / NIC-1890). Free and unlimited — no plan is passed
	// in. projectOwnerVerifier keeps note ↛ project at the type level.
	noteSvc := note.NewService(noteRepo, projectOwnerVerifier{projects: projectSvc}, ws.NewNoteBroadcaster(wsHub)).
		WithCleaner(attachmentSvc)

	// Close the bucket→note seam now that the note service exists (E-053 /
	// NIC-1903). Post-construction for the same reason as the task cleaner: the
	// concretes meet only here in wiring.
	bucketSvc = bucketSvc.WithNoteCreator(noteSvc)

	// Habits (E-055 / NIC-1923). Own domain rather than a recurrence_rules
	// flavour: habits belong to no project and never materialize task rows.
	habitSvc := habit.NewService(habit.NewRepository(pool), ws.NewHabitBroadcaster(wsHub))

	// AI tool executor (NIC-ai-tool-use). Wired post-construction so the task
	// service is already fully composed (cleaner/materializer/focusTotals) before
	// the executor calls into it. The executor is only useful when the provider
	// is enabled — a disabled aiClient still gets it, but the kill switch keeps
	// every /v1/ai/* endpoint at 503, so it never runs.
	aiSvc = aiSvc.WithExecutor(ai.NewToolExecutor(
		aiTaskAdapter{tasks: taskSvc},
		aiProjectAdapter{projects: projectSvc},
	))

	// Google Calendar connection (E-052 / NIC-1844). Any credential or the
	// encryption key missing ⇒ every endpoint returns a typed 503 and nothing
	// else in the app notices.
	googleCalSvc, googleEventsSvc, googleCalendarsSvc := newGoogleCalServices(cfg, pool, authSvc)

	// Sweep jobs — hourly, invoked by Render Cron Jobs via /internal/jobs/*.
	jobsRepo := jobs.NewRepository(pool)
	dueDateNotifier := jobs.NewDueDateNotifier(jobsRepo, notificationSvc, cfg.SMTPDsn)
	overdueNotifier := jobs.NewOverdueNotifier(jobsRepo, notificationSvc)
	dayStartNotifier := jobs.NewDayStartNotifier(jobsRepo, notificationSvc)
	inboxNotifier := jobs.NewInboxNotifier(jobsRepo, notificationSvc)
	summaryNotifier := jobs.NewSummaryNotifier(jobsRepo, notificationSvc)

	handlers := handler.Handlers{
		Auth:            auth.NewHandler(authSvc, cookieCfg),
		Area:            area.NewHandler(areaSvc),
		Project:         project.NewHandler(projectSvc),
		Task:            task.NewHandler(taskSvc, subtaskSvc),
		Bucket:          bucket.NewHandler(bucketSvc),
		AI:              ai.NewHandler(aiSvc),
		Billing:         billing.NewHandler(nil),
		Search:          search.NewHandler(searchSvc),
		Attachment:      attachment.NewHandler(attachmentSvc),
		Recurrence:      recurrence.NewHandler(recurrenceSvc),
		Focus:           focus.NewHandler(focusSvc),
		Note:            note.NewHandler(noteSvc),
		Habit:           habit.NewHandler(habitSvc),
		Notification:    notification.NewHandler(notificationSvc),
		GoogleCal:       googlecal.NewHandler(googleCalSvc, cfg.AppBaseURL),
		GoogleEvents:    googlecal.NewEventsHandler(googleEventsSvc),
		GoogleCalendars: googlecal.NewCalendarsHandler(googleCalendarsSvc),
		Jobs: jobs.NewHandler(dueDateNotifier, overdueNotifier, dayStartNotifier, inboxNotifier, summaryNotifier, attachmentGCAdapter{svc: attachmentSvc}).
			WithRecurrence(recurrenceSweepAdapter{m: recurrenceMaterializer}).
			WithFocusStale(focusStaleAdapter{svc: focusSvc}).
			WithAIToolExpiry(aiToolExpiryAdapter{repo: aiRepo, ttl: 7 * 24 * time.Hour}),
		WS: ws.NewHandler(wsHub, cfg.JWTSecret, cfg.CORSOrigins),
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

// ownerVerifier adapts the task and note domains to attachment.OwnerVerifier,
// dispatching on the polymorphic owner type. Each branch resolves ownership with
// a user-scoped lookup and normalizes any not-found into RESOURCE_NOT_FOUND, so
// a foreign or missing owner never leaks its existence.
type ownerVerifier struct {
	tasks task.Service
	notes note.Repository
}

func (v ownerVerifier) VerifyOwner(ctx context.Context, userID, ownerType, ownerID string) error {
	switch ownerType {
	case attachment.OwnerTypeTask:
		if _, err := v.tasks.Get(ctx, userID, ownerID); err != nil {
			if ae, ok := errors.AsType[*apperror.AppError](err); ok && ae.Status == http.StatusNotFound {
				return notOwned()
			}
			return err
		}
		return nil

	case attachment.OwnerTypeNote:
		owned, err := v.notes.ExistsForUser(ctx, userID, ownerID)
		if err != nil {
			return err
		}
		if !owned {
			return notOwned()
		}
		return nil

	default:
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "unknown owner type")
	}
}

// notOwned is the single not-found an owner check may return. A foreign owner and
// a missing one are indistinguishable by design — a 403 would confirm the row
// exists.
func notOwned() error {
	return apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "resource not found")
}

// projectOwnerVerifier adapts the project service to note.ProjectOwnershipVerifier.
// It resolves ownership with a user-scoped lookup and normalizes any not-found
// (project-scoped or otherwise) into RESOURCE_NOT_FOUND, so filing a note into
// someone else's project is indistinguishable from filing into a missing one.
type projectOwnerVerifier struct {
	projects project.Service
}

func (v projectOwnerVerifier) VerifyProjectOwner(ctx context.Context, userID, projectID string) error {
	if _, err := v.projects.Get(ctx, userID, projectID); err != nil {
		if ae, ok := errors.AsType[*apperror.AppError](err); ok && ae.Status == http.StatusNotFound {
			return apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "resource not found")
		}
		return err
	}
	return nil
}

// ownerExistence adapts the task and note repos to attachment.OwnerExistence for
// the GC sweep — a system-wide (no user scope) existence check. An unknown owner
// type is reported non-existent so its stale rows get reaped.
type ownerExistence struct {
	tasks task.Repository
	notes note.Repository
}

func (e ownerExistence) OwnerExists(ctx context.Context, ownerType, ownerID string) (bool, error) {
	switch ownerType {
	case attachment.OwnerTypeTask:
		return e.tasks.ExistsByID(ctx, ownerID)
	case attachment.OwnerTypeNote:
		return e.notes.ExistsByID(ctx, ownerID)
	default:
		return false, nil
	}
}

// focusStaleAdapter adapts the focus service to jobs.FocusStaleSweeper,
// translating the domain breakdown into the jobs-facing one so neither package
// imports the other.
type focusStaleAdapter struct {
	svc focus.Service
}

func (a focusStaleAdapter) SweepStale(ctx context.Context, dryRun bool) (jobs.FocusSweepResult, error) {
	res, err := a.svc.SweepStale(ctx, dryRun)
	if err != nil {
		return jobs.FocusSweepResult{}, err
	}
	return jobs.FocusSweepResult{
		Considered: res.Considered,
		Closed:     res.Closed,
		DryRun:     res.DryRun,
	}, nil
}

// recurrenceSweepAdapter adapts the recurrence materializer to jobs.RecurrenceSweep,
// translating the domain result into the jobs-facing one so neither package
// imports the other.
type recurrenceSweepAdapter struct {
	m *recurrence.Materializer
}

func (a recurrenceSweepAdapter) Run(ctx context.Context, dryRun bool) (*jobs.RecurrenceResult, error) {
	res, err := a.m.Run(ctx, dryRun)
	if err != nil {
		return nil, err
	}
	return &jobs.RecurrenceResult{
		Considered:       res.Considered,
		Materialized:     res.Materialized,
		Reaped:           res.Reaped,
		SkippedPlanLimit: res.SkippedPlanLimit,
		SkippedExisting:  res.SkippedExisting,
		SkippedNotDue:    res.SkippedNotDue,
		SkippedBadZone:   res.SkippedBadZone,
	}, nil
}

// aiTaskAdapter adapts the task service to ai.TaskCommands so the ai domain
// never imports task. Each method boxes the concrete task.TaskView /
// task.ListTasksResponse into the opaque JSON-carrier the executor uses; the
// AI package re-marshals through json.Marshal, staying oblivious to the wire
// schema of another domain.
type aiTaskAdapter struct {
	tasks task.Service
}

func (a aiTaskAdapter) Get(ctx context.Context, userID, id string) (ai.TaskViewJSON, error) {
	v, err := a.tasks.Get(ctx, userID, id)
	if err != nil {
		return ai.TaskViewJSON{}, err
	}
	return ai.TaskViewJSON{Value: v}, nil
}

func (a aiTaskAdapter) Create(ctx context.Context, userID, projectID, plan string, req ai.CreateTaskInput) (ai.TaskViewJSON, error) {
	v, err := a.tasks.Create(ctx, userID, projectID, plan, task.CreateTaskRequest{
		Title:            req.Title,
		Notes:            req.Notes,
		Status:           req.Status,
		Priority:         req.Priority,
		Energy:           req.Energy,
		RollsOver:        req.RollsOver,
		ScheduledFor:     req.ScheduledFor,
		ScheduledTime:    req.ScheduledTime,
		EstimatedMinutes: req.EstimatedMinutes,
		URL:              req.URL,
	})
	if err != nil {
		return ai.TaskViewJSON{}, err
	}
	return ai.TaskViewJSON{Value: v}, nil
}

func (a aiTaskAdapter) SetStatus(ctx context.Context, userID, id, plan, status string) (ai.TaskViewJSON, error) {
	v, err := a.tasks.SetStatus(ctx, userID, id, plan, status)
	if err != nil {
		return ai.TaskViewJSON{}, err
	}
	return ai.TaskViewJSON{Value: v}, nil
}

func (a aiTaskAdapter) Schedule(ctx context.Context, userID, id, plan string, req ai.ScheduleInput) (ai.TaskViewJSON, error) {
	v, err := a.tasks.Schedule(ctx, userID, id, plan, task.ScheduleRequest{
		ScheduledFor: req.ScheduledFor, ScheduledTime: req.ScheduledTime, RollsOver: req.RollsOver,
	})
	if err != nil {
		return ai.TaskViewJSON{}, err
	}
	return ai.TaskViewJSON{Value: v}, nil
}

func (a aiTaskAdapter) ListForUser(ctx context.Context, userID string, f ai.UserListInput) (ai.TaskListJSON, error) {
	list, err := a.tasks.ListForUser(ctx, userID, task.UserListFilter{
		Status: f.Status, Priority: f.Priority, Energy: f.Energy,
		ProjectID: f.ProjectID, ScheduledFrom: f.ScheduledFrom, ScheduledTo: f.ScheduledTo,
		Search: f.Search, Limit: f.Limit,
	})
	if err != nil {
		return ai.TaskListJSON{}, err
	}
	return ai.TaskListJSON{Value: list}, nil
}

// aiProjectAdapter adapts the project service to ai.ProjectCommands. Returns a
// large page (200) — the ai list_projects tool is a "list everything" surface,
// and 200 covers every real user; if it ever isn't enough the tool description
// steers the model to narrow with list_tasks + projectId instead.
type aiProjectAdapter struct {
	projects project.Service
}

const aiProjectListLimit = 200

func (a aiProjectAdapter) List(ctx context.Context, userID string) (ai.ProjectListJSON, error) {
	list, err := a.projects.List(ctx, userID, project.ListProjectsFilter{Limit: aiProjectListLimit})
	if err != nil {
		return ai.ProjectListJSON{}, err
	}
	return ai.ProjectListJSON{Value: list}, nil
}

// aiToolExpiryAdapter adapts the ai repository to jobs.AIToolExpirySweeper so
// the nightly cron can flip 7-day-stale pending proposals to 'expired' without
// jobs importing the ai domain.
type aiToolExpiryAdapter struct {
	repo ai.Repository
	ttl  time.Duration
}

func (a aiToolExpiryAdapter) ExpireStale(ctx context.Context) (int, error) {
	return a.repo.ExpirePendingOlderThan(ctx, time.Now().Add(-a.ttl))
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

// newGoogleCalServices wires the Google Calendar connection and events services
// (E-052).
//
// Extracted from main so the wiring reads as one decision rather than a dozen
// lines of plumbing. Any missing credential makes the client disabled and every
// endpoint degrade — connection endpoints return a typed 503, the events
// endpoint returns an empty overlay with `disconnected` — so an environment
// without Google configuration boots normally.
//
// Both services share one repository and one client: they are two views of the
// same connection, and a second client would mean a second set of credentials to
// keep in step.
func newGoogleCalServices(cfg config.Config, pool *pgxpool.Pool, authSvc auth.Service) (googlecal.Service, googlecal.EventsService, googlecal.CalendarService) {
	cipher, err := cryptoutil.NewCipher(cfg.GoogleTokenEncKey)
	if err != nil {
		// A key that is present but malformed must not boot: silently disabling
		// encryption would store live refresh tokens in plaintext.
		log.Fatal().Err(err).Msg("invalid GOOGLE_TOKEN_ENC_KEY")
	}
	if cfg.GoogleEnabled() {
		log.Info().Msg("google calendar: enabled")
	} else {
		log.Warn().Msg("google calendar: disabled (unset GOOGLE_* env) — /v1/calendar/google/* returns 503")
	}

	repo := googlecal.NewRepository(pool, cipher)
	client := google.New(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.GoogleRedirectURL)

	svc := googlecal.NewService(repo, googlecal.NewStateRepository(pool), client)

	// Deleting a Nicoflow account must not leave a live Google grant behind
	// (E-040 / GDPR). Easy to miss because deletion is a SOFT delete — the user
	// row survives, so nothing else forces the connection to be dealt with.
	if registry, ok := authSvc.(auth.EraserRegistry); ok {
		registry.RegisterEraser(svc)
	}

	// The events service owns the cache; the picker invalidates it on a
	// selection change so the overlay reflects the new set immediately.
	eventsSvc := googlecal.NewEventsService(repo, client)

	return svc, eventsSvc, googlecal.NewCalendarService(repo, client, eventsSvc)
}
