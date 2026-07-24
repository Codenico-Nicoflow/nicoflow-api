package bucket

// Domain event types the bucket service emits. The ws adapter maps these onto
// the wire event names; the domain never imports internal/ws. EventTaskCreated
// is emitted here (not by the task service) on the process→task path, so both
// events fire together only after the whole ordered operation succeeds.
const (
	EventCreated     = "bucket.created"
	EventProcessed   = "bucket.processed"
	EventDeleted     = "bucket.deleted"
	EventTaskCreated = "task.created"
)

// Event is the full-payload real-time event for a bucket mutation. Payload is
// the BucketView (created/processed), the TaskView (task.created), or a Ref
// for delete.
type Event struct {
	Type    string
	Payload any
}

// Ref identifies a bucket item by id alone (delete payload).
type Ref struct {
	ID string `json:"id"`
}

// Broadcaster pushes a bucket event to the user's live connections. Nil
// disables emission; satisfied by the ws adapter at wire-up. Emits fire only
// after the whole mutation has succeeded — a mid-operation failure emits
// nothing.
type Broadcaster interface {
	Broadcast(userID string, ev Event)
}

func (s *service) emit(userID string, ev Event) {
	if s.broadcaster == nil {
		return
	}
	s.broadcaster.Broadcast(userID, ev)
}
