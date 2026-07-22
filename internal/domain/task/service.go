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
	// freePlanTaskLimit counts only active+inbox tasks per project (calm limit).
	freePlanTaskLimit = 50

	statusDone     = "done"
	defaultStatus  = "inbox"
	defaultPriorty = "medium"
	defaultEnergy  = "medium"

	maxTitleLen = 255
	maxNotesLen = 2000
	maxURLLen   = 2048

	// scheduledForLayout is the ISO date format for the soft scheduledFor field.
	scheduledForLayout = "2006-01-02"
)

var (
	allowedStatuses   = map[string]bool{"inbox": true, "active": true, "someday": true, "done": true, "cancelled": true}
	allowedPriorities = map[string]bool{"low": true, "medium": true, "high": true}
	allowedEnergies   = map[string]bool{"low": true, "medium": true, "deep": true}
	// activeInboxStatuses are the statuses that count against the plan limit.
	activeInboxStatuses = map[string]bool{"active": true, "inbox": true}
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
	Schedule(ctx context.Context, userID, id string, req ScheduleRequest) (TaskView, error)
	ReorderOne(ctx context.Context, userID, id string, displayOrder int) (TaskView, error)
	Focus(ctx context.Context, userID string, p FocusParams) (ListTasksResponse, error)
	TimeSpread(ctx context.Context, userID string, loc *time.Location) (TimeSpreadResponse, error)
}

type service struct {
	repo        Repository
	now         func() time.Time // injectable clock — Focus/Time-Spread read time only through this
	notif       notifier         // best-effort notification emitter; nil disables emission
	broadcaster Broadcaster      // real-time WS emitter; nil disables emission
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
	return TaskToView(*t), nil
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

	owned, err := s.repo.ProjectOwned(ctx, userID, projectID)
	if err != nil {
		return TaskView{}, err
	}
	if !owned {
		return TaskView{}, apperror.New(http.StatusNotFound, apperror.ErrProjectNotFound, "project not found")
	}

	// Plan limit: a new active/inbox task must not exceed the free cap.
	if plan == "free" && activeInboxStatuses[req.Status] {
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
		EstimatedMinutes: req.EstimatedMinutes,
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

	current, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		return TaskView{}, err
	}

	// Plan limit applies when a PATCH moves a task INTO active/inbox.
	if plan == "free" && req.Status != nil &&
		activeInboxStatuses[*req.Status] && !activeInboxStatuses[current.Status] {
		if err := s.enforceTaskLimit(ctx, userID, current.ProjectID); err != nil {
			return TaskView{}, err
		}
	}

	transition := completedAtTransition(current.Status, req.Status)
	updated, err := s.repo.Update(ctx, userID, id, req, transition)
	if err != nil {
		return TaskView{}, err
	}

	// Real-time producers fire only on the transition INTO done, after the write
	// commits. Best-effort — see notify.go.
	if transition == completedAtSetNow {
		s.emitTaskCompleted(ctx, updated)
		s.emitProjectCompletedIfLast(ctx, updated)
	}
	return TaskToView(updated), nil
}

func (s *service) Delete(ctx context.Context, userID, id string) error {
	if err := s.repo.Delete(ctx, userID, id); err != nil {
		return err
	}
	s.emit(userID, Event{Type: EventDeleted, Payload: Ref{ID: id}})
	return nil
}

const (
	defaultFocusLimit = 5
	maxFocusLimit     = 20
)

// Focus returns a deterministically-ranked short list of the user's active+inbox
// tasks that fit the given time/energy. Candidate set spans all projects;
// someday/done/cancelled are excluded at the repo. Scoring is pure (focus.go).
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

	candidates, err := s.repo.ListActiveInboxByUser(ctx, userID)
	if err != nil {
		return ListTasksResponse{}, err
	}
	ranked := rankFocus(candidates, p, s.now())

	items := make([]TaskView, len(ranked))
	for i, t := range ranked {
		items[i] = TaskToView(t)
	}
	return ListTasksResponse{Items: items}, nil
}

// TimeSpread buckets the user's active+inbox tasks into today/tomorrow/this-week
// with the no-guilt roll-forward. Bucketing is pure (timespread.go); the clock
// is injected so tests are reproducible. `loc` sets which day boundary counts as
// "today" — the injected now is anchored to it before bucketing.
func (s *service) TimeSpread(ctx context.Context, userID string, loc *time.Location) (TimeSpreadResponse, error) {
	if loc == nil {
		loc = time.UTC
	}
	candidates, err := s.repo.ListActiveInboxByUser(ctx, userID)
	if err != nil {
		return TimeSpreadResponse{}, err
	}
	return bucketTimeSpread(candidates, s.now().In(loc)), nil
}

// SetStatus is a shorthand for a status-only PATCH (checkbox toggle, move to
// someday, etc.). It reuses Update so completedAt side-effects and the plan
// limit on moving into active/inbox are applied identically.
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

// Schedule sets (or clears) the soft scheduledFor intention + rollsOver flag.
func (s *service) Schedule(ctx context.Context, userID, id string, req ScheduleRequest) (TaskView, error) {
	if err := validateScheduledFor(req.ScheduledFor); err != nil {
		return TaskView{}, err
	}
	t, err := s.repo.UpdateSchedule(ctx, userID, id, req.ScheduledFor, req.RollsOver)
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
	count, err := s.repo.CountActiveInbox(ctx, userID, projectID)
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
	return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidStatus, "status must be one of: inbox, active, someday, done, cancelled")
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
