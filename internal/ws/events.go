package ws

import "time"

// EventType identifies the kind of real-time event pushed to the client.
type EventType string

const (
	EventTaskCreated       EventType = "task.created"
	EventTaskUpdated       EventType = "task.updated"
	EventTaskDeleted       EventType = "task.deleted"
	EventTaskStatusChanged EventType = "task.status_changed"
	EventProjectCreated    EventType = "project.created"
	EventProjectUpdated    EventType = "project.updated"
	EventProjectDeleted    EventType = "project.deleted"
	EventAreaCreated       EventType = "area.created"
	EventAreaUpdated       EventType = "area.updated"
	EventAreaDeleted       EventType = "area.deleted"
	EventBucketCreated     EventType = "bucket.created"
	EventBucketProcessed   EventType = "bucket.processed"
	EventBucketDeleted     EventType = "bucket.deleted"
	EventAttachmentCreated EventType = "attachment.created"
	EventAttachmentDeleted EventType = "attachment.deleted"
	// EventNotificationCreated carries a full NotificationView. Renamed from the
	// scaffold's "notification" to match the frontend event map (E-023) and the
	// shape notification.Service already emits.
	EventNotificationCreated EventType = "notification.created"
	// EventAISessionUpdated fires when an AI session gains an assistant reply,
	// carrying { id }. The client refetches the session on receipt.
	EventAISessionUpdated EventType = "ai.session.updated"
	// Recurrence rule events (E-050) carry a full RuleView; deleted carries { id }.
	// FREE on every plan.
	EventRecurrenceCreated EventType = "recurrence.created"
	EventRecurrenceUpdated EventType = "recurrence.updated"
	EventRecurrenceDeleted EventType = "recurrence.deleted"
	// Focus timer events (E-049) carry a full SessionView. Transition-only —
	// heartbeats never broadcast. FREE on every plan.
	EventFocusSessionStarted EventType = "focus.session_started"
	EventFocusSessionEnded   EventType = "focus.session_ended"
	// Note events (E-053) carry the LIST-shaped NoteView — excerpt, never the
	// document body. note.updated deliberately breaks the full-payload
	// convention: autosave fires every ~1–2s while typing, and shipping a whole
	// rich-text body on each keystroke burst would be wasteful and racy. A client
	// that needs the body refetches the scalar. The event also has a correctness
	// role beyond cache sync — it lets a second tab notice a change and refetch
	// before the user types, defusing conflicts before `version` has to reject
	// them. deleted carries { id }. FREE on every plan.
	EventNoteCreated EventType = "note.created"
	EventNoteUpdated EventType = "note.updated"
	EventNoteDeleted EventType = "note.deleted"
)

// Event is the envelope for every server-pushed WebSocket message.
type Event struct {
	Event     EventType `json:"event"`
	Payload   any       `json:"payload"`
	Timestamp time.Time `json:"timestamp"`
}
