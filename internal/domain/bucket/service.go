package bucket

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/note"
	"github.com/nicoflow/nicoflow-api/internal/domain/task"
)

// maxContentLen is the hard limit on bucket item content (matches VARCHAR(500)).
const maxContentLen = 500

// planPro is the plan value that unlocks the Pro-only inbox_zero notification.
const planPro = "pro"

// NoteCreator is the slice of the note service the bucket domain depends on to
// turn a processed item into a note. Defined here (the consumer) so the bucket
// package never imports the concrete note service.
type NoteCreator interface {
	Create(ctx context.Context, userID string, req note.CreateNoteRequest) (note.NoteDetailView, error)
}

// TaskCreator is the slice of the task service the bucket domain depends on to
// turn a processed item into a task. Defined here (the consumer) so the bucket
// package never imports the concrete task service. CreateWithoutEvent (not
// Create) so the task.created emit is deferred to the end of the ordered
// process operation — both events or neither, never one mid-way.
type TaskCreator interface {
	CreateWithoutEvent(ctx context.Context, userID, projectID, plan string, req task.CreateTaskRequest) (task.TaskView, error)
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

	// WithNoteCreator injects the note service used by the "note" process result.
	WithNoteCreator(n NoteCreator) Service
	Process(ctx context.Context, userID, id, plan string, req ProcessBucketRequest) (BucketView, error)
}

type service struct {
	repo        Repository
	taskSvc     TaskCreator
	noteSvc     NoteCreator // nil ⇒ note processing unavailable
	notif       notifier    // best-effort notification emitter; nil disables emission
	broadcaster Broadcaster // nil disables real-time emission
}

// NewService creates a new bucket service. notif may be nil (notifications are
// best-effort); pass notification.Service to enable inbox_zero emission.
// broadcaster may be nil (real-time emission disabled); pass the ws adapter to
// light up live updates.
func NewService(repo Repository, taskSvc TaskCreator, notif notifier, broadcaster Broadcaster) Service {
	return &service{repo: repo, taskSvc: taskSvc, notif: notif, broadcaster: broadcaster}
}

// WithNoteCreator enables processing an item into a note. A post-construction
// option rather than a constructor argument so the existing wiring is untouched
// and a nil creator stays an explicit, testable "unavailable" state.
func (s *service) WithNoteCreator(n NoteCreator) Service {
	s.noteSvc = n
	return s
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
	view := BucketToView(created)
	s.emit(userID, Event{Type: EventCreated, Payload: view})
	return view, nil
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
	s.emit(userID, Event{Type: EventDeleted, Payload: Ref{ID: id}})
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
		marked, err := s.repo.MarkProcessed(ctx, userID, id, ResultTrash, ProcessedRefs{})
		if err != nil {
			return BucketView{}, err
		}
		view := BucketToView(marked)
		s.emit(userID, Event{Type: EventProcessed, Payload: view})
		s.maybeEmitInboxZero(ctx, userID, plan)
		return view, nil
	case ResultNote:
		return s.processToNote(ctx, userID, id, plan, req)
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

	// Create the task first — without its task.created emit. Its service enforces
	// title validation, project ownership (PROJECT_NOT_FOUND), and the free-plan
	// task cap (PLAN_LIMIT_EXCEEDED); any of these abort before the item is marked.
	created, err := s.taskSvc.CreateWithoutEvent(ctx, userID, *req.ProjectID, plan, req.TaskDetails.toTaskCreateRequest())
	if err != nil {
		return BucketView{}, err
	}

	taskID := created.ID
	marked, err := s.repo.MarkProcessed(ctx, userID, id, ResultTask, ProcessedRefs{TaskID: &taskID, ProjectID: req.ProjectID})
	if err != nil {
		return BucketView{}, err
	}

	// Both writes succeeded — fire both events together (both-or-neither).
	view := BucketToView(marked)
	s.emit(userID, Event{Type: EventProcessed, Payload: view})
	s.emit(userID, Event{Type: EventTaskCreated, Payload: created})
	s.maybeEmitInboxZero(ctx, userID, plan)
	return view, nil
}

// processToNote mirrors processToTask: ordered, NOT wrapped in a shared
// transaction. The note is created first (its own tx — title validation and
// project ownership abort before the bucket row is touched), and only then is
// the item marked processed. A processed item with no note would be a silently
// emptied inbox entry; a benign orphan note if the final mark loses a race is
// visible and user-fixable.
func (s *service) processToNote(ctx context.Context, userID, id, plan string, req ProcessBucketRequest) (BucketView, error) {
	if req.ProjectID == nil || *req.ProjectID == "" {
		return BucketView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "projectId is required to process into a note")
	}
	if req.NoteDetails == nil {
		return BucketView{}, apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, "noteDetails is required to process into a note")
	}
	if s.noteSvc == nil {
		return BucketView{}, apperror.New(http.StatusNotImplemented, apperror.ErrServiceUnavailable, "note processing is not available")
	}

	// Fail fast on missing/already-processed before creating any note, so the
	// common error cases never leave an orphan.
	existing, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		return BucketView{}, err
	}
	if existing.ProcessedAt != nil {
		return BucketView{}, apperror.New(http.StatusConflict, apperror.ErrConflict, "bucket item is already processed")
	}

	created, err := s.noteSvc.Create(ctx, userID, req.NoteDetails.toNoteCreateRequest(*req.ProjectID))
	if err != nil {
		return BucketView{}, err
	}

	noteID := created.ID
	marked, err := s.repo.MarkProcessed(ctx, userID, id, ResultNote, ProcessedRefs{NoteID: &noteID, ProjectID: req.ProjectID})
	if err != nil {
		return BucketView{}, err
	}

	view := BucketToView(marked)
	s.emit(userID, Event{Type: EventProcessed, Payload: view})
	s.maybeEmitInboxZero(ctx, userID, plan)
	return view, nil
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
