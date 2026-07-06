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
	Update(ctx context.Context, userID, id, plan string, req UpdateTaskRequest) (TaskView, error)
	Delete(ctx context.Context, userID, id string) error
	SetStatus(ctx context.Context, userID, id, plan, status string) (TaskView, error)
	Schedule(ctx context.Context, userID, id string, req ScheduleRequest) (TaskView, error)
	ReorderOne(ctx context.Context, userID, id string, displayOrder int) (TaskView, error)
	Focus(ctx context.Context, userID string, p FocusParams) (ListTasksResponse, error)
	TimeSpread(ctx context.Context, userID string) (TimeSpreadResponse, error)
}

type service struct {
	repo Repository
	now  func() time.Time // injectable clock — Focus/Time-Spread read time only through this
}

// NewService creates a new task service with a real clock.
func NewService(repo Repository) Service {
	return &service{repo: repo, now: time.Now}
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
		DueDate:          req.DueDate,
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

	updated, err := s.repo.Update(ctx, userID, id, req, completedAtTransition(current.Status, req.Status))
	if err != nil {
		return TaskView{}, err
	}
	return TaskToView(updated), nil
}

func (s *service) Delete(ctx context.Context, userID, id string) error {
	return s.repo.Delete(ctx, userID, id)
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
// is injected so tests are reproducible.
func (s *service) TimeSpread(ctx context.Context, userID string) (TimeSpreadResponse, error) {
	candidates, err := s.repo.ListActiveInboxByUser(ctx, userID)
	if err != nil {
		return TimeSpreadResponse{}, err
	}
	return bucketTimeSpread(candidates, s.now()), nil
}

// SetStatus is a shorthand for a status-only PATCH (checkbox toggle, move to
// someday, etc.). It reuses Update so completedAt side-effects and the plan
// limit on moving into active/inbox are applied identically.
func (s *service) SetStatus(ctx context.Context, userID, id, plan, status string) (TaskView, error) {
	if status == "" {
		return TaskView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "status is required")
	}
	return s.Update(ctx, userID, id, plan, UpdateTaskRequest{Status: &status})
}

// Schedule sets (or clears) the soft scheduledFor intention + rollsOver flag.
func (s *service) Schedule(ctx context.Context, userID, id string, req ScheduleRequest) (TaskView, error) {
	if req.ScheduledFor != nil {
		if _, err := time.Parse(scheduledForLayout, *req.ScheduledFor); err != nil {
			return TaskView{}, apperror.New(http.StatusBadRequest, apperror.ErrInvalidDate, "scheduledFor must be an ISO date (YYYY-MM-DD)")
		}
	}
	t, err := s.repo.UpdateSchedule(ctx, userID, id, req.ScheduledFor, req.RollsOver)
	if err != nil {
		return TaskView{}, err
	}
	return TaskToView(t), nil
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
	return TaskToView(t), nil
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
