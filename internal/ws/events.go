package ws

import "time"

// EventType identifies the kind of real-time event pushed to the client.
type EventType string

const (
	EventTaskCreated    EventType = "task.created"
	EventTaskUpdated    EventType = "task.updated"
	EventTaskDeleted    EventType = "task.deleted"
	EventProjectCreated EventType = "project.created"
	EventProjectUpdated EventType = "project.updated"
	EventProjectDeleted EventType = "project.deleted"
	EventAreaCreated    EventType = "area.created"
	EventAreaUpdated    EventType = "area.updated"
	EventAreaDeleted    EventType = "area.deleted"
	EventNotification   EventType = "notification"
)

// Event is the envelope for every server-pushed WebSocket message.
type Event struct {
	Event     EventType `json:"event"`
	Payload   any       `json:"payload"`
	Timestamp time.Time `json:"timestamp"`
}
