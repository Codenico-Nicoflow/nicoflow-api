package area

// Domain event types the area service emits. The ws adapter maps these onto the
// wire event names; the domain never imports internal/ws.
const (
	EventCreated = "area.created"
	EventUpdated = "area.updated"
	EventDeleted = "area.deleted"
)

// Event is the full-payload real-time event for an area mutation. Payload is the
// AreaView, or a Ref for delete.
type Event struct {
	Type    string
	Payload any
}

// Ref identifies an area by id alone (delete payload).
type Ref struct {
	ID string `json:"id"`
}

// Broadcaster pushes an area event to the user's live connections. Nil disables
// emission; satisfied by the ws adapter at wire-up. Emits fire only after the
// whole mutation has succeeded.
type Broadcaster interface {
	Broadcast(userID string, ev Event)
}

func (s *service) emit(userID string, ev Event) {
	if s.broadcaster == nil {
		return
	}
	s.broadcaster.Broadcast(userID, ev)
}
