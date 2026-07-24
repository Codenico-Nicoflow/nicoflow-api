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
)

// Event is the envelope for every server-pushed WebSocket message.
type Event struct {
	Event     EventType `json:"event"`
	Payload   any       `json:"payload"`
	Timestamp time.Time `json:"timestamp"`
}
