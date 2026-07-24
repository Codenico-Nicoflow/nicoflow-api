package task

// Domain event types the task service emits. The ws adapter maps these onto the
// wire event names; the domain never imports internal/ws.
const (
	EventCreated       = "task.created"
	EventUpdated       = "task.updated"
	EventDeleted       = "task.deleted"
	EventStatusChanged = "task.status_changed"
)

// Event is the full-payload real-time event for a task mutation. Payload is the
// TaskView for create/update/status_changed, or a Ref where the full view is
// gone or out of reach (delete; subtask-driven parent updates).
type Event struct {
	Type    string
	Payload any
}

// Ref identifies a task by id alone. The client reacts by invalidating and
// refetching, so the id is all it needs.
type Ref struct {
	ID string `json:"id"`
}

// Broadcaster pushes a task event to the user's live connections. Nil disables
// emission; satisfied by the ws adapter at wire-up. Emits fire only after the
// whole mutation has succeeded — never mid-operation.
type Broadcaster interface {
	Broadcast(userID string, ev Event)
}

// emit fires ev if a broadcaster is wired. Fire-and-forget by design: the hub
// never blocks or fails the mutation path.
func (s *service) emit(userID string, ev Event) {
	if s.broadcaster == nil {
		return
	}
	s.broadcaster.Broadcast(userID, ev)
}
