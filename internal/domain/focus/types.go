// Package focus owns time-on-task measurement (E-049). A focus session is one
// contiguous active run on a single task — a "segment". Pausing closes a
// segment; resuming opens a new one. A task's total time is the SUM over its
// closed segments, derived on read rather than cached, so no counter can drift
// away from the rows that justify it.
package focus

import (
	"context"
	"time"
)

// Domain event types emitted on segment transitions. Heartbeats deliberately do
// not broadcast — they carry no state change a client could act on. The ws
// adapter maps these onto the wire names through an explicit table.
const (
	EventSessionStarted = "focus.session_started"
	EventSessionEnded   = "focus.session_ended"
)

// Session is the internal domain model — one contiguous run on one task.
// EndedAt nil marks the single open segment a user may have at any moment.
// LastSeen is the newest heartbeat the server accepted; a close stamps it into
// EndedAt, so an abandoned segment counts the time it proved rather than the
// time until a sweep noticed it.
type Session struct {
	ID        string
	UserID    string
	TaskID    string
	StartedAt time.Time
	EndedAt   *time.Time
	LastSeen  time.Time
}

// IsOpen reports whether the segment is still running.
func (s Session) IsOpen() bool { return s.EndedAt == nil }

// DurationSeconds is the segment's measured length, 0 while it is still open —
// an open segment has no truthful server-side end yet, and the client renders
// the live tick from StartedAt itself.
func (s Session) DurationSeconds() int64 {
	if s.EndedAt == nil {
		return 0
	}
	d := s.EndedAt.Sub(s.StartedAt)
	if d < 0 {
		return 0
	}
	return int64(d.Seconds())
}

// SessionView is the JSON response shape and the WS payload for both
// focus.session_started and focus.session_ended. All IDs are strings; instants
// are RFC3339 UTC.
type SessionView struct {
	ID              string  `json:"id"`
	TaskID          string  `json:"taskId"`
	StartedAt       string  `json:"startedAt"`
	EndedAt         *string `json:"endedAt"`
	LastSeen        string  `json:"lastSeen"`
	DurationSeconds int64   `json:"durationSeconds"`
}

// ToView maps the domain model to its JSON shape.
func ToView(s Session) SessionView {
	v := SessionView{
		ID:              s.ID,
		TaskID:          s.TaskID,
		StartedAt:       s.StartedAt.UTC().Format(time.RFC3339),
		LastSeen:        s.LastSeen.UTC().Format(time.RFC3339),
		DurationSeconds: s.DurationSeconds(),
	}
	if s.EndedAt != nil {
		e := s.EndedAt.UTC().Format(time.RFC3339)
		v.EndedAt = &e
	}
	return v
}

// OpenSessionRequest is the body for opening a segment. Only the task is named:
// every timestamp is stamped server-side, because a client-supplied duration is
// unverifiable and would make the whole measurement worthless.
type OpenSessionRequest struct {
	TaskID string `json:"taskId"`
}

// SweepBreakdown reports one stale-sweep run for the internal job's response and
// its log line. Scanned counts the stale open segments found; Closed counts the
// ones this run actually closed — the two differ when a segment was closed
// concurrently by its own client between the scan and the close.
type SweepBreakdown struct {
	Scanned int `json:"scanned"`
	Closed  int `json:"closed"`
}

// Service is the focus domain's business-logic contract consumed by the handler.
type Service interface {
	// Open starts a segment on req.TaskID, closing any other segment the user has
	// open. Timestamps are server-stamped; a client-supplied duration is ignored.
	Open(ctx context.Context, userID string, req OpenSessionRequest) (SessionView, error)
	// CloseCurrent closes the user's open segment, or reports not-found when there
	// is none.
	CloseCurrent(ctx context.Context, userID string) (SessionView, error)
	// Heartbeat bumps the open segment's last_seen. Silent — never broadcasts.
	Heartbeat(ctx context.Context, userID string) error
}

// TaskOwnershipChecker is the narrow slice of the task domain this package needs:
// may this user start a timer on this task? Defined here (the consumer) and
// satisfied by task.Repository, so focus never imports the task package.
type TaskOwnershipChecker interface {
	IsOpenable(ctx context.Context, userID, id string) (bool, error)
}

// Broadcaster receives a domain Event for real-time fan-out. Fire-and-forget:
// implementations must never block or fail the mutation. A nil Broadcaster is a
// valid no-op seam.
type Broadcaster interface {
	Broadcast(userID string, ev Event)
}

// Event is the domain-level real-time event. The ws adapter maps Type onto the
// wire EventType.
type Event struct {
	Type    string
	Payload any
}

// Repository is the data-access contract, defined here in the consuming package
// per project layering; the pgx implementation lives in repository.go. Every
// method is row-scoped by user_id except ListStaleOpen and CloseByID, which are
// explicitly system-scope and reachable only from the internal sweep.
type Repository interface {
	// OpenAtomic starts a segment on taskID and closes any other open segment for
	// the user in the same transaction, so the one-open-per-user invariant never
	// has an observable window in which two rows are open. Concurrent opens for
	// one user serialize rather than racing: the loser closes the winner's segment
	// and opens its own, so the caller never sees a unique-index error. Returns the
	// new open segment and the one it closed (nil when there was none) — the caller
	// broadcasts an ended event for the latter.
	OpenAtomic(ctx context.Context, s Session) (opened Session, closed *Session, err error)

	// GetOpenByUser returns the user's currently open segment. ok is false when
	// none is open, which is an ordinary state, not an error.
	GetOpenByUser(ctx context.Context, userID string) (s Session, ok bool, err error)

	// CloseOpenByUser closes the user's open segment, stamping ended_at from
	// last_seen. ok is false when nothing was open, making a double-close a
	// no-op rather than an error.
	CloseOpenByUser(ctx context.Context, userID string) (s Session, ok bool, err error)

	// TouchLastSeen bumps the open segment's heartbeat and returns the updated
	// row. ok is false when the segment is gone or already closed — the client
	// treats that as "your timer stopped", never as a failure.
	TouchLastSeen(ctx context.Context, userID, id string) (s Session, ok bool, err error)

	// TouchCurrent bumps whichever segment the user has open, in one statement.
	// The heartbeat targets "my current segment", so resolving the id first would
	// add a round trip and a window in which a sweep could close the row between
	// the read and the write. ok is false when nothing is open.
	TouchCurrent(ctx context.Context, userID string) (s Session, ok bool, err error)

	// ListStaleOpen returns open segments whose last_seen predates cutoff, oldest
	// first, capped at limit. System-scope: the sweep must see every user's rows.
	ListStaleOpen(ctx context.Context, cutoff time.Time, limit int) ([]Session, error)

	// CloseByID closes one segment by id, stamping ended_at from last_seen.
	// Idempotent: ok is false when the row is missing or already closed.
	// System-scope, sweep-only — the user-scoped path is CloseOpenByUser.
	CloseByID(ctx context.Context, id string) (s Session, ok bool, err error)

	// SumClosedSecondsByTask totals a task's closed segments, 0 when none exist.
	// The open segment is excluded: its end is not yet known.
	SumClosedSecondsByTask(ctx context.Context, userID, taskID string) (int64, error)

	// SumClosedSecondsByTaskBatch is the list-view fast path — one round trip for
	// many tasks. Tasks with no closed segments are absent from the map, which
	// callers read as 0.
	SumClosedSecondsByTaskBatch(ctx context.Context, userID string, taskIDs []string) (map[string]int64, error)
}
