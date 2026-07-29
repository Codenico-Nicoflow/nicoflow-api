package focus

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// StaleThreshold is how long an open segment may go without a heartbeat before
// the sweep considers it abandoned. Three times the ~30s client cadence, so a
// single dropped heartbeat (or a slow network) never costs the user their timer.
const StaleThreshold = 90 * time.Second

// maxStalePerSweep caps one sweep run. Anything beyond it is picked up on the
// next tick, so a backlog can never turn a single run into an unbounded job.
const maxStalePerSweep = 500

type service struct {
	repo        Repository
	tasks       TaskOwnershipChecker
	broadcaster Broadcaster      // nil disables emission
	now         func() time.Time // injectable clock — only the sweep cutoff reads it
}

// NewService creates a focus Service with a real clock. broadcaster may be nil
// (real-time emission disabled); pass the ws adapter to light up live updates.
func NewService(repo Repository, tasks TaskOwnershipChecker, broadcaster Broadcaster) Service {
	return &service{repo: repo, tasks: tasks, broadcaster: broadcaster, now: time.Now}
}

// NewServiceWithClock is like NewService but with an injected clock, so the
// stale-sweep cutoff is deterministic in tests.
func NewServiceWithClock(repo Repository, tasks TaskOwnershipChecker, broadcaster Broadcaster, now func() time.Time) Service {
	return &service{repo: repo, tasks: tasks, broadcaster: broadcaster, now: now}
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
// SweepStale is crash recovery: it closes segments whose client stopped
// heartbeating and never sent a close. Each is closed at its own last_seen —
// never the sweep's wall clock and never a fixed cap — so a stranded session
// contributes neither phantom time nor a truncated total, whether it was
// abandoned after 2 minutes or genuinely ran for three hours.
func (s *service) SweepStale(ctx context.Context, dryRun bool) (SweepBreakdown, error) {
	cutoff := s.now().Add(-StaleThreshold)
	stale, err := s.repo.ListStaleOpen(ctx, cutoff, maxStalePerSweep)
	if err != nil {
		return SweepBreakdown{}, err
	}

	out := SweepBreakdown{Considered: len(stale), DryRun: dryRun}
	if dryRun {
		return out, nil
	}

	for _, seg := range stale {
		// Per-item resilience: one row that fails to close must not strand the
		// rest. CloseByID is idempotent, so the next tick retries this one.
		closed, ok, err := s.repo.CloseByID(ctx, seg.ID)
		if err != nil {
			log.Error().Err(err).Str("session_id", seg.ID).Msg("focus: stale close failed")
			continue
		}
		if !ok {
			// Closed by its own client between the scan and here — not an error.
			continue
		}
		out.Closed++
		s.emit(closed.UserID, Event{Type: EventSessionEnded, Payload: ToView(closed)})
	}
	return out, nil
}

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
