package task

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

const (
	// FreePlanTaskLimit counts only active tasks per project (calm limit).
	// Exported so the recurrence materializer enforces the same ceiling rather
	// than duplicating the number.
	FreePlanTaskLimit = 50
	freePlanTaskLimit = FreePlanTaskLimit

	// planFree is the JWT plan claim for a free user — the value every plan gate
	// in this package compares against.
	planFree = "free"

	statusActive    = "active"
	statusDone      = "done"
	statusCancelled = "cancelled"
	defaultStatus   = statusActive
	defaultPriorty  = "medium"
	defaultEnergy   = "medium"

	maxTitleLen = 255
	maxNotesLen = 2000
	maxURLLen   = 2048

	// scheduledForLayout is the ISO date format for the soft scheduledFor field.
	scheduledForLayout = "2006-01-02"
)

var (
	allowedStatuses   = map[string]bool{statusActive: true, statusDone: true, statusCancelled: true}
	allowedPriorities = map[string]bool{"low": true, "medium": true, "high": true}
	allowedEnergies   = map[string]bool{"low": true, "medium": true, "deep": true}
)

// Service defines the task business logic interface.
type Service interface {
	ListByProject(ctx context.Context, userID, projectID string, f ListTasksFilter) (ListTasksResponse, error)
	Get(ctx context.Context, userID, id string) (TaskView, error)
	Create(ctx context.Context, userID, projectID, plan string, req CreateTaskRequest) (TaskView, error)
	// CreateWithoutEvent is Create minus the task.created emit — for callers that
	// compose the create into a larger operation (bucket process) and emit the
	// event themselves only once the whole operation succeeds (both-or-neither).
	CreateWithoutEvent(ctx context.Context, userID, projectID, plan string, req CreateTaskRequest) (TaskView, error)
	Update(ctx context.Context, userID, id, plan string, req UpdateTaskRequest) (TaskView, error)
	Delete(ctx context.Context, userID, id string) error
	SetStatus(ctx context.Context, userID, id, plan, status string) (TaskView, error)
	Schedule(ctx context.Context, userID, id, plan string, req ScheduleRequest) (TaskView, error)
	// ListByDateRange returns the user's tasks scheduled within an inclusive date
	// window, in calendar-grid order and with no roll-forward applied.
	ListByDateRange(ctx context.Context, userID, from, to string) (ListTasksResponse, error)
	ReorderOne(ctx context.Context, userID, id string, displayOrder int) (TaskView, error)
	Focus(ctx context.Context, userID string, p FocusParams) (ListTasksResponse, error)
	TimeSpread(ctx context.Context, userID string, loc *time.Location) (TimeSpreadResponse, error)
	// WithCleaner injects the attachment cleaner invoked best-effort on delete
	// and returns the service for chaining. Wired once in main.go.
	WithCleaner(c AttachmentCleaner) Service
	// WithMaterializer injects the recurrence materializer invoked best-effort
	// when a recurring occurrence is completed. Wired once in main.go.
	WithMaterializer(m RecurrenceMaterializer) Service
	// WithFocusTotals injects the focus-totals reader that enriches Focus +
	// GetTask responses with totalFocusSeconds. Wired once in main.go.
	WithFocusTotals(f FocusTotals) Service
	// ListForUser is the cross-project user-scoped read backing the AI tool
	// executor's list_tasks. See user_list.go.
	ListForUser(ctx context.Context, userID string, f UserListFilter) (ListTasksResponse, error)
}

type service struct {
	repo        Repository
	now         func() time.Time  // injectable clock — Focus/Time-Spread read time only through this
	notif       notifier          // best-effort notification emitter; nil disables emission
	broadcaster Broadcaster       // real-time WS emitter; nil disables emission
	cleaner     AttachmentCleaner // best-effort attachment cleanup on delete; nil disables
	// best-effort recurrence successor on completion; nil disables (cron still catches it)
	materializer RecurrenceMaterializer
	focusTotals  FocusTotals // enriches Focus/GetTask with totalFocusSeconds; nil = zero-default
}

// WithCleaner injects the attachment cleaner used on task delete. Kept as a
// post-construction option so the many NewService callers/tests need no change;
// wired once in main.go, where the attachment service concrete is available.
func (s *service) WithCleaner(c AttachmentCleaner) Service {
	s.cleaner = c
	return s
}

// WithMaterializer injects the recurrence materializer used on completion. Kept
// as a post-construction option for the same reason as WithCleaner: the two
// services reference each other, so the concretes only meet in main.go.
func (s *service) WithMaterializer(m RecurrenceMaterializer) Service {
	s.materializer = m
	return s
}

// WithFocusTotals injects the focus-totals reader, same post-construction
// pattern as above: task and focus meet only in main.go wiring.
func (s *service) WithFocusTotals(f FocusTotals) Service {
	s.focusTotals = f
	return s
}

// NewService creates a new task service with a real clock. notif may be nil
// (notifications are best-effort); pass notification.Service to enable emission.
// broadcaster may be nil (real-time emission disabled); pass the ws adapter to
// light up live updates.
func NewService(repo Repository, notif notifier, broadcaster Broadcaster) Service {
	return &service{repo: repo, now: time.Now, notif: notif, broadcaster: broadcaster}
}

// NewServiceWithClock is like NewService but with an injected clock, for
// deterministic tests of time-dependent endpoints (Focus, Time-Spread).
func NewServiceWithClock(repo Repository, now func() time.Time) Service {
	return &service{repo: repo, now: now}
}

func (s *service) ListByProject(ctx context.Context, userID, projectID string, f ListTasksFilter) (ListTasksResponse, error) {
	if f.Status != nil && !allowedStatuses[*f.Status] {
		return ListTasksResponse{}, errInvalidStatus()
	}
	if f.Priority != nil && !allowedPriorities[*f.Priority] {
		return ListTasksResponse{}, errInvalidPriority()
	}
	if f.Energy != nil && !allowedEnergies[*f.Energy] {
		return ListTasksResponse{}, errInvalidEnergy()
	}

	owned, err := s.repo.ProjectOwned(ctx, userID, projectID)
	if err != nil {
		return ListTasksResponse{}, err
	}
	if !owned {
		return ListTasksResponse{}, apperror.New(http.StatusNotFound, apperror.ErrProjectNotFound, "project not found")
	}

	tasks, err := s.repo.ListByProject(ctx, userID, projectID, f)
	if err != nil {
		return ListTasksResponse{}, err
	}
	items := make([]TaskView, len(tasks))
	for i, t := range tasks {
		items[i] = TaskToView(t)
	}
	return ListTasksResponse{Items: items}, nil
}

func (s *service) Get(ctx context.Context, userID, id string) (TaskView, error) {
	t, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		return TaskView{}, err
	}
	view := TaskToView(*t)
	if s.focusTotals != nil {
		total, err := s.focusTotals.SumClosedSecondsByTask(ctx, userID, id)
		if err != nil {
			return TaskView{}, fmt.Errorf("task focus total: %w", err)
		}
		view.TotalFocusSeconds = total
	}
	return view, nil
}

func (s *service) Create(ctx context.Context, userID, projectID, plan string, req CreateTaskRequest) (TaskView, error) {
	view, err := s.CreateWithoutEvent(ctx, userID, projectID, plan, req)
	if err != nil {
		return TaskView{}, err
	}
	s.emit(userID, Event{Type: EventCreated, Payload: view})
	return view, nil
}

func (s *service) CreateWithoutEvent(ctx context.Context, userID, projectID, plan string, req CreateTaskRequest) (TaskView, error) {
	req, rollsOver, err := normalizeCreate(req)
	if err != nil {
		return TaskView{}, err
	}
	if err := enforceTimedSchedulingPlan(plan, req.ScheduledTime); err != nil {
		return TaskView{}, err
	}

	owned, err := s.repo.ProjectOwned(ctx, userID, projectID)
	if err != nil {
		return TaskView{}, err
	}
	if !owned {
		return TaskView{}, apperror.New(http.StatusNotFound, apperror.ErrProjectNotFound, "project not found")
	}

	// Plan limit: a new active task must not exceed the free cap.
	if plan == "free" && req.Status == statusActive {
		if err := s.enforceTaskLimit(ctx, userID, projectID); err != nil {
			return TaskView{}, err
		}
	}

	order, err := s.repo.NextDisplayOrder(ctx, userID, projectID)
	if err != nil {
		return TaskView{}, err
	}

	t := Task{
		ID:               uuid.New().String(),
		UserID:           userID,
		ProjectID:        projectID,
		Title:            req.Title,
		Notes:            req.Notes,
		Status:           req.Status,
		Priority:         req.Priority,
		Energy:           req.Energy,
		RollsOver:        rollsOver,
		ScheduledFor:     req.ScheduledFor,
		ScheduledTime:    req.ScheduledTime,
		EstimatedMinutes: clampEstimateToDayEnd(req.ScheduledTime, req.EstimatedMinutes),
		URL:              req.URL,
		DisplayOrder:     order,
	}
	if req.Status == statusDone {
		t.CompletedAt = ptrNow()
	}

	created, err := s.repo.Create(ctx, t)
	if err != nil {
		return TaskView{}, err
	}
	return TaskToView(created), nil
}

func (s *service) Update(ctx context.Context, userID, id, plan string, req UpdateTaskRequest) (TaskView, error) {
	view, err := s.update(ctx, userID, id, plan, req)
	if err != nil {
		return TaskView{}, err
	}
	s.emit(userID, Event{Type: EventUpdated, Payload: view})
	return view, nil
}

// update is the shared PATCH body behind Update and SetStatus. It emits no
// real-time event itself — the callers do, so the edit endpoint fires
// task.updated and the status endpoint task.status_changed, never both.
func (s *service) update(ctx context.Context, userID, id, plan string, req UpdateTaskRequest) (TaskView, error) {
	if err := validateUpdate(&req); err != nil {
		return TaskView{}, err
	}
	// Only a PATCH that actually sets a time is gated; an absent field and an
	// explicit null both leave a free user unblocked.
	if req.ScheduledTime.Set {
		if err := enforceTimedSchedulingPlan(plan, req.ScheduledTime.Value); err != nil {
			return TaskView{}, err
		}
	}

	current, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		return TaskView{}, err
	}
	// Clamp against the EFFECTIVE pair: either side may be untouched by this
	// PATCH, so a new estimate must still respect an already-stored time.
	req.EstimatedMinutes = clampUpdateEstimate(req, *current)

	// Plan limit applies when a PATCH moves a task INTO active.
	if plan == "free" && req.Status != nil &&
		*req.Status == statusActive && current.Status != statusActive {
		if err := s.enforceTaskLimit(ctx, userID, current.ProjectID); err != nil {
			return TaskView{}, err
		}
	}

	transition := completedAtTransition(current.Status, req.Status)
	updated, err := s.repo.Update(ctx, userID, id, req, transition)
	if err != nil {
		return TaskView{}, err
	}

	view := TaskToView(updated)

	// Real-time producers fire only on the transition INTO done, after the write
	// commits. Best-effort — see notify.go.
	if transition == completedAtSetNow {
		s.emitTaskCompleted(ctx, updated)
		s.emitProjectCompletedIfLast(ctx, updated)
		// Here rather than in SetStatus: the edit dialog completes a task through
		// the plain PATCH, which would otherwise leave the series with no successor
		// until the hourly sweep.
		s.materializeSuccessor(ctx, userID, view)
	}
	return view, nil
}

func (s *service) Delete(ctx context.Context, userID, id string) error {
	if err := s.repo.Delete(ctx, userID, id); err != nil {
		return err
	}
	// Best-effort: a cleanup failure is logged but never fails the delete — the
	// task is already gone and the GC sweep reaps any leftover attachments.
	s.cleanAttachments(ctx, userID, id)
	s.emit(userID, Event{Type: EventDeleted, Payload: Ref{ID: id}})
	return nil
}

const (
	defaultFocusLimit = 5
	maxFocusLimit     = 20
)

// Focus returns a deterministically-ranked short list of the user's active
// tasks that fit the given time/energy. Candidate set spans all projects;
// done/cancelled are excluded at the repo. Scoring is pure (focus.go).
func (s *service) Focus(ctx context.Context, userID string, p FocusParams) (ListTasksResponse, error) {
	if p.Energy != "" && !allowedEnergies[p.Energy] {
		return ListTasksResponse{}, errInvalidEnergy()
	}
	if p.Available < 0 {
		return ListTasksResponse{}, apperror.New(http.StatusBadRequest, apperror.ErrInvalidInput, "available must be zero or greater")
	}
	if p.Limit <= 0 {
		p.Limit = defaultFocusLimit
	}
	if p.Limit > maxFocusLimit {
		p.Limit = maxFocusLimit
	}

	candidates, err := s.repo.ListActiveByUser(ctx, userID)
	if err != nil {
		return ListTasksResponse{}, err
	}
	ranked := rankFocus(candidates, p, s.now())

	items := make([]TaskView, len(ranked))
	for i, t := range ranked {
		items[i] = TaskToView(t)
	}
	if err := s.enrichFocusTotals(ctx, userID, items); err != nil {
		return ListTasksResponse{}, err
	}
	return ListTasksResponse{Items: items}, nil
}

// ListByDateRange backs the calendar grid. Deliberately NOT an extension of
// Time Spread: Time Spread rolls a missed task forward onto today, which is
// exactly wrong for a calendar — a month view must show a task on the day it
// was actually scheduled for. Not Pro-gated; this is only tasks by date, and
// the gate lives on writing a time.
func (s *service) ListByDateRange(ctx context.Context, userID, from, to string) (ListTasksResponse, error) {
	window, err := parseDateRange(from, to)
	if err != nil {
		return ListTasksResponse{}, err
	}
	tasks, err := s.repo.ListByDateRange(ctx, userID, window.From, window.To)
	if err != nil {
		return ListTasksResponse{}, err
	}
	items := make([]TaskView, len(tasks))
	for i, t := range tasks {
		items[i] = TaskToView(t)
	}
	return ListTasksResponse{Items: items}, nil
}

// TimeSpread buckets the user's active tasks into today/tomorrow/this-week
// with the no-guilt roll-forward. Bucketing is pure (timespread.go); the clock
// is injected so tests are reproducible. `loc` sets which day boundary counts as
// "today" — the injected now is anchored to it before bucketing.
func (s *service) TimeSpread(ctx context.Context, userID string, loc *time.Location) (TimeSpreadResponse, error) {
	if loc == nil {
		loc = time.UTC
	}
	candidates, err := s.repo.ListActiveByUser(ctx, userID)
	if err != nil {
		return TimeSpreadResponse{}, err
	}
	return bucketTimeSpread(candidates, s.now().In(loc)), nil
}

// SetStatus is a shorthand for a status-only PATCH (checkbox toggle, cancel,
// etc.). It reuses Update so completedAt side-effects and the plan limit on
// moving into active are applied identically.
func (s *service) SetStatus(ctx context.Context, userID, id, plan, status string) (TaskView, error) {
	if status == "" {
		return TaskView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "status is required")
	}
	view, err := s.update(ctx, userID, id, plan, UpdateTaskRequest{Status: &status})
	if err != nil {
		return TaskView{}, err
	}
	s.emit(userID, Event{Type: EventStatusChanged, Payload: view})
	return view, nil
}

// validateScheduledFor rejects a non-ISO scheduledFor. It is a soft date
// (YYYY-MM-DD), never an enum — nil means "unset" and passes.
func validateScheduledFor(scheduledFor *string) error {
	if scheduledFor == nil {
		return nil
	}
	if _, err := time.Parse(scheduledForLayout, *scheduledFor); err != nil {
		return apperror.New(http.StatusBadRequest, apperror.ErrInvalidDate, "scheduledFor must be an ISO date (YYYY-MM-DD)")
	}
	return nil
}

// Schedule sets (or clears) the soft scheduledFor intention, the optional
// time-of-day, and the rollsOver flag. Setting a time is Pro-gated; clearing it
// is open on every plan.
func (s *service) Schedule(ctx context.Context, userID, id, plan string, req ScheduleRequest) (TaskView, error) {
	if err := validateScheduledFor(req.ScheduledFor); err != nil {
		return TaskView{}, err
	}
	if err := validateScheduledTime(req.ScheduledTime); err != nil {
		return TaskView{}, err
	}
	if err := enforceTimedSchedulingPlan(plan, req.ScheduledTime); err != nil {
		return TaskView{}, err
	}
	// A time without a day has nowhere to land — the grid is keyed by date.
	if req.ScheduledFor == nil && req.ScheduledTime != nil {
		return TaskView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput,
			"scheduledTime requires a scheduledFor date")
	}

	t, err := s.repo.UpdateSchedule(ctx, userID, id, req.ScheduledFor, req.ScheduledTime, req.RollsOver)
	if err != nil {
		return TaskView{}, err
	}
	view := TaskToView(t)
	s.emit(userID, Event{Type: EventUpdated, Payload: view})
	return view, nil
}

// ReorderOne moves a task to a target order and re-packs its siblings within
// the project so orders stay contiguous.
func (s *service) ReorderOne(ctx context.Context, userID, id string, displayOrder int) (TaskView, error) {
	if displayOrder < 0 {
		return TaskView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "displayOrder must be zero or greater")
	}
	t, err := s.repo.Repack(ctx, userID, id, displayOrder)
	if err != nil {
		return TaskView{}, err
	}
	view := TaskToView(t)
	s.emit(userID, Event{Type: EventUpdated, Payload: view})
	return view, nil
}

func (s *service) enforceTaskLimit(ctx context.Context, userID, projectID string) error {
	count, err := s.repo.CountActive(ctx, userID, projectID)
	if err != nil {
		return fmt.Errorf("task plan-limit count: %w", err)
	}
	if count >= freePlanTaskLimit {
		return apperror.New(http.StatusForbidden, apperror.ErrPlanLimitExceeded, "free plan allows up to 50 active tasks per project")
	}
	return nil
}

// completedAtTransition decides how completed_at must change for a status update.
func completedAtTransition(currentStatus string, newStatus *string) completedAtChange {
	if newStatus == nil || *newStatus == currentStatus {
		return completedAtKeep
	}
	switch {
	case *newStatus == statusDone:
		return completedAtSetNow
	case currentStatus == statusDone:
		return completedAtClear
	default:
		return completedAtKeep
	}
}

// normalizeCreate trims/validates the title, applies enum defaults, and resolves
// rollsOver. Returns the normalized request + the effective rollsOver value.
func normalizeCreate(req CreateTaskRequest) (CreateTaskRequest, bool, error) {
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		return req, false, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "title is required")
	}
	if len(req.Title) > maxTitleLen {
		return req, false, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "title must be 255 characters or fewer")
	}

	if req.Status == "" {
		req.Status = defaultStatus
	} else if !allowedStatuses[req.Status] {
		return req, false, errInvalidStatus()
	}
	if req.Priority == "" {
		req.Priority = defaultPriorty
	} else if !allowedPriorities[req.Priority] {
		return req, false, errInvalidPriority()
	}
	if req.Energy == "" {
		req.Energy = defaultEnergy
	} else if !allowedEnergies[req.Energy] {
		return req, false, errInvalidEnergy()
	}

	if err := validateOptional(req.Notes, req.EstimatedMinutes, req.URL); err != nil {
		return req, false, err
	}
	if err := validateScheduledFor(req.ScheduledFor); err != nil {
		return req, false, err
	}
	if err := validateScheduledTime(req.ScheduledTime); err != nil {
		return req, false, err
	}

	rollsOver := true
	if req.RollsOver != nil {
		rollsOver = *req.RollsOver
	}
	return req, rollsOver, nil
}

// validateUpdate trims the title and validates any provided enum/optional fields.
func validateUpdate(req *UpdateTaskRequest) error {
	if req.Title != nil {
		*req.Title = strings.TrimSpace(*req.Title)
		if *req.Title == "" {
			return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "title cannot be empty")
		}
		if len(*req.Title) > maxTitleLen {
			return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "title must be 255 characters or fewer")
		}
	}
	if req.Status != nil && !allowedStatuses[*req.Status] {
		return errInvalidStatus()
	}
	if req.Priority != nil && !allowedPriorities[*req.Priority] {
		return errInvalidPriority()
	}
	if req.Energy != nil && !allowedEnergies[*req.Energy] {
		return errInvalidEnergy()
	}
	if err := validateScheduledFor(req.ScheduledFor.Value); err != nil {
		return err
	}
	if err := validateScheduledTime(req.ScheduledTime.Value); err != nil {
		return err
	}
	return validateOptional(req.Notes.Value, req.EstimatedMinutes.Value, req.URL.Value)
}

func validateOptional(notes *string, estimatedMinutes *int, url *string) error {
	if notes != nil && len(*notes) > maxNotesLen {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "notes must be 2000 characters or fewer")
	}
	if estimatedMinutes != nil && (*estimatedMinutes < 1 || *estimatedMinutes > 1440) {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "estimatedMinutes must be between 1 and 1440")
	}
	if url != nil && len(*url) > maxURLLen {
		return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "url is too long")
	}
	return nil
}

func errInvalidStatus() error {
	return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidStatus, "status must be one of: active, done, cancelled")
}

func errInvalidPriority() error {
	return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidPriority, "priority must be one of: low, medium, high")
}

func errInvalidEnergy() error {
	return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "energy must be one of: low, medium, deep")
}

func ptrNow() *time.Time {
	t := time.Now().UTC()
	return &t
}
