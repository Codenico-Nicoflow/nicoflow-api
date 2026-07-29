package focus

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// StaleThreshold is how long an open segment may go without a heartbeat before
// the sweep considers it abandoned. Three times the ~30s client cadence, so a
// single dropped heartbeat (or a slow network) never costs the user their timer.
const StaleThreshold = 90 * time.Second

type service struct {
	repo        Repository
	tasks       TaskOwnershipChecker
	broadcaster Broadcaster // nil disables emission
}

// NewService creates a focus Service. broadcaster may be nil (real-time emission
// disabled); pass the ws adapter to light up live updates.
func NewService(repo Repository, tasks TaskOwnershipChecker, broadcaster Broadcaster) Service {
	return &service{repo: repo, tasks: tasks, broadcaster: broadcaster}
}

// emit fans a domain event out best-effort. A nil broadcaster is a valid no-op.
func (s *service) emit(userID string, ev Event) {
	if s.broadcaster != nil {
		s.broadcaster.Broadcast(userID, ev)
	}
}

// Open starts a segment on taskID, closing any other segment the user has open.
// Every timestamp is stamped by the database — a client-supplied duration is
// unverifiable, so it is never accepted.
func (s *service) Open(ctx context.Context, userID string, req OpenSessionRequest) (SessionView, error) {
	taskID := strings.TrimSpace(req.TaskID)
	if taskID == "" {
		return SessionView{}, apperror.New(http.StatusBadRequest, apperror.ErrInvalidInput, "taskId is required")
	}

	// Ownership + non-terminal gate. A task the caller does not own is reported
	// exactly like a terminal or missing one — no existence oracle.
	openable, err := s.tasks.IsOpenable(ctx, userID, taskID)
	if err != nil {
		return SessionView{}, err
	}
	if !openable {
		return SessionView{}, apperror.New(http.StatusNotFound, apperror.ErrTaskNotFound, "task not found")
	}

	opened, closed, err := s.repo.OpenAtomic(ctx, Session{
		ID: uuid.NewString(), UserID: userID, TaskID: taskID,
	})
	if err != nil {
		return SessionView{}, err
	}

	// Emitted only after the transaction committed, and ended-before-started so a
	// listening tab stops the old timer before it starts the new one. Reversing
	// them would leave a moment where the client believes two segments run.
	if closed != nil {
		s.emit(userID, Event{Type: EventSessionEnded, Payload: ToView(*closed)})
	}
	s.emit(userID, Event{Type: EventSessionStarted, Payload: ToView(opened)})

	return ToView(opened), nil
}

// CloseCurrent closes the user's open segment. Absence of one is a 404 rather
// than a silent success: the client asked to stop something specific, and
// pretending it worked would hide a desynced timer.
func (s *service) CloseCurrent(ctx context.Context, userID string) (SessionView, error) {
	closed, ok, err := s.repo.CloseOpenByUser(ctx, userID)
	if err != nil {
		return SessionView{}, err
	}
	if !ok {
		return SessionView{}, apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "no open focus session")
	}
	view := ToView(closed)
	s.emit(userID, Event{Type: EventSessionEnded, Payload: view})
	return view, nil
}

// Heartbeat bumps the open segment's last_seen. Deliberately silent: a heartbeat
// carries no state change a client could act on, and broadcasting one every 30s
// per user would be pure noise.
func (s *service) Heartbeat(ctx context.Context, userID string) error {
	_, ok, err := s.repo.TouchCurrent(ctx, userID)
	if err != nil {
		return err
	}
	if !ok {
		// Nothing open — either it never was, or a sweep closed it. Either way the
		// timer really is stopped, and the client should stop ticking.
		return apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "no open focus session")
	}
	return nil
}
