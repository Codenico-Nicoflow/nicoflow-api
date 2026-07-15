package bucket

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/task"
)

// maxContentLen is the hard limit on bucket item content (matches VARCHAR(500)).
const maxContentLen = 500

// planPro is the plan value that unlocks the Pro-only inbox_zero notification.
const planPro = "pro"

// TaskCreator is the slice of the task service the bucket domain depends on to
// turn a processed item into a task. Defined here (the consumer) so the bucket
// package never imports the concrete task service. The real *task.service
// satisfies it.
type TaskCreator interface {
	Create(ctx context.Context, userID, projectID, plan string, req task.CreateTaskRequest) (task.TaskView, error)
}

// Service defines the bucket (inbox) business logic interface.
type Service interface {
	Create(ctx context.Context, userID, content string) (BucketView, error)
	List(ctx context.Context, userID string) (BucketListResponse, error)
	Get(ctx context.Context, userID, id string) (BucketView, error)
	Update(ctx context.Context, userID, id, content string) (BucketView, error)
	// Delete takes plan so it can fire the Pro inbox_zero notification when
	// deleting an item clears the last unprocessed one.
	Delete(ctx context.Context, userID, id, plan string) error
	Process(ctx context.Context, userID, id, plan string, req ProcessBucketRequest) (BucketView, error)
}

type service struct {
	repo    Repository
	taskSvc TaskCreator
	notif   notifier // best-effort notification emitter; nil disables emission
}

// NewService creates a new bucket service. notif may be nil (notifications are
// best-effort); pass notification.Service to enable inbox_zero emission.
func NewService(repo Repository, taskSvc TaskCreator, notif notifier) Service {
	return &service{repo: repo, taskSvc: taskSvc, notif: notif}
}

func (s *service) Create(ctx context.Context, userID, content string) (BucketView, error) {
	content, err := validateContent(content)
	if err != nil {
		return BucketView{}, err
	}
	created, err := s.repo.Create(ctx, Bucket{
		ID:      uuid.New().String(),
		UserID:  userID,
		Content: content,
	})
	if err != nil {
		return BucketView{}, err
	}
	return BucketToView(created), nil
}

func (s *service) List(ctx context.Context, userID string) (BucketListResponse, error) {
	items, err := s.repo.ListByUser(ctx, userID)
	if err != nil {
		return BucketListResponse{}, err
	}
	views := make([]BucketView, 0, len(items))
	for _, b := range items {
		views = append(views, BucketToView(b))
	}
	return BucketListResponse{Items: views}, nil
}

func (s *service) Get(ctx context.Context, userID, id string) (BucketView, error) {
	b, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		return BucketView{}, err
	}
	return BucketToView(b), nil
}

func (s *service) Update(ctx context.Context, userID, id, content string) (BucketView, error) {
	content, err := validateContent(content)
	if err != nil {
		return BucketView{}, err
	}
	updated, err := s.repo.UpdateContent(ctx, userID, id, content)
	if err != nil {
		return BucketView{}, err
	}
	return BucketToView(updated), nil
}

func (s *service) Delete(ctx context.Context, userID, id, plan string) error {
	if err := s.repo.Delete(ctx, userID, id); err != nil {
		return err
	}
	// Deleting an unprocessed item can clear the inbox — emit inbox_zero if so.
	s.maybeEmitInboxZero(ctx, userID, plan)
	return nil
}

// Process turns an unprocessed inbox item into a task, trashes it, or (future)
// a note. It is ordered, NOT wrapped in a shared DB transaction: for the task
// path the task is created first (its own tx — validation, PROJECT_NOT_FOUND,
// and PLAN_LIMIT_EXCEEDED all abort before the bucket is touched), and only
// then is the item marked processed. This guarantees we never leave a processed
// bucket item without a task; the worst case is a benign orphan task if the
// final mark loses a concurrency race.
func (s *service) Process(ctx context.Context, userID, id, plan string, req ProcessBucketRequest) (BucketView, error) {
	switch req.ProcessingResult {
	case ResultTask:
		return s.processToTask(ctx, userID, id, plan, req)
	case ResultTrash:
		marked, err := s.repo.MarkProcessed(ctx, userID, id, ResultTrash, nil, nil)
		if err != nil {
			return BucketView{}, err
		}
		s.maybeEmitInboxZero(ctx, userID, plan)
		return BucketToView(marked), nil
	case ResultNote:
		return BucketView{}, apperror.New(http.StatusNotImplemented, apperror.ErrServiceUnavailable, "note processing is not implemented")
	default:
		return BucketView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "processingResult must be one of: task, note, trash")
	}
}

func (s *service) processToTask(ctx context.Context, userID, id, plan string, req ProcessBucketRequest) (BucketView, error) {
	if req.ProjectID == nil || *req.ProjectID == "" {
		return BucketView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "projectId is required to process into a task")
	}
	if req.TaskDetails == nil {
		return BucketView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "taskDetails is required to process into a task")
	}

	// Fail fast on missing/already-processed before creating any task, so the
	// common error cases never leave an orphan.
	existing, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		return BucketView{}, err
	}
	if existing.ProcessedAt != nil {
		return BucketView{}, apperror.New(http.StatusConflict, apperror.ErrConflict, "bucket item is already processed")
	}

	// Create the task first. Its service enforces title validation, project
	// ownership (PROJECT_NOT_FOUND), and the free-plan task cap
	// (PLAN_LIMIT_EXCEEDED); any of these abort before the item is marked.
	created, err := s.taskSvc.Create(ctx, userID, *req.ProjectID, plan, req.TaskDetails.toTaskCreateRequest())
	if err != nil {
		return BucketView{}, err
	}

	taskID := created.ID
	marked, err := s.repo.MarkProcessed(ctx, userID, id, ResultTask, &taskID, req.ProjectID)
	if err != nil {
		return BucketView{}, err
	}
	s.maybeEmitInboxZero(ctx, userID, plan)
	return BucketToView(marked), nil
}

// validateContent trims and enforces the 1..500 length rule (counted in runes).
func validateContent(content string) (string, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "content must not be empty")
	}
	if utf8.RuneCountInString(trimmed) > maxContentLen {
		return "", apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "content must be at most 500 characters")
	}
	return trimmed, nil
}
