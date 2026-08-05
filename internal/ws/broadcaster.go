package ws

import (
	"time"

	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/domain/ai"
	"github.com/nicoflow/nicoflow-api/internal/domain/area"
	"github.com/nicoflow/nicoflow-api/internal/domain/attachment"
	"github.com/nicoflow/nicoflow-api/internal/domain/bucket"
	"github.com/nicoflow/nicoflow-api/internal/domain/focus"
	"github.com/nicoflow/nicoflow-api/internal/domain/habit"
	"github.com/nicoflow/nicoflow-api/internal/domain/note"
	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
	"github.com/nicoflow/nicoflow-api/internal/domain/project"
	"github.com/nicoflow/nicoflow-api/internal/domain/recurrence"
	"github.com/nicoflow/nicoflow-api/internal/domain/task"
)

// The adapters below satisfy each domain's Broadcaster interface, mapping the
// domain's event type string onto the wire EventType and fanning the payload out
// over the Hub. Adapters live here, in ws, importing the domains — never the
// reverse — keeping every domain package transport-agnostic. Fire-and-forget:
// BroadcastToUser silently skips a user with no live connection, so a broadcast
// never blocks or fails a mutation path.

// wireTypes maps every domain event type string a service can emit onto its wire
// EventType. Domain constants equal the wire names by convention; the map (not a
// cast) keeps that an explicit, auditable contract — an unmapped type is a
// programming error, logged and dropped rather than sent mangled.
var wireTypes = map[string]EventType{
	task.EventCreated:       EventTaskCreated,
	task.EventUpdated:       EventTaskUpdated,
	task.EventDeleted:       EventTaskDeleted,
	task.EventStatusChanged: EventTaskStatusChanged,
	project.EventCreated:    EventProjectCreated,
	project.EventUpdated:    EventProjectUpdated,
	project.EventDeleted:    EventProjectDeleted,
	area.EventCreated:       EventAreaCreated,
	area.EventUpdated:       EventAreaUpdated,
	area.EventDeleted:       EventAreaDeleted,
	// bucket.EventTaskCreated shares task.EventCreated's key ("task.created") —
	// the entry above covers the bucket process→task emit too.
	bucket.EventCreated:       EventBucketCreated,
	bucket.EventProcessed:     EventBucketProcessed,
	bucket.EventDeleted:       EventBucketDeleted,
	attachment.EventCreated:   EventAttachmentCreated,
	attachment.EventDeleted:   EventAttachmentDeleted,
	recurrence.EventCreated:   EventRecurrenceCreated,
	recurrence.EventUpdated:   EventRecurrenceUpdated,
	recurrence.EventDeleted:   EventRecurrenceDeleted,
	ai.EventSessionUpdated:    EventAISessionUpdated,
	focus.EventSessionStarted: EventFocusSessionStarted,
	focus.EventSessionEnded:   EventFocusSessionEnded,
	note.EventCreated:         EventNoteCreated,
	note.EventUpdated:         EventNoteUpdated,
	note.EventDeleted:         EventNoteDeleted,
	habit.EventCreated:        EventHabitCreated,
	habit.EventUpdated:        EventHabitUpdated,
	habit.EventDeleted:        EventHabitDeleted,
	habit.EventCheckedIn:      EventHabitCheckedIn,
	"notification.created":    EventNotificationCreated,
}

// broadcast resolves the wire type and pushes the envelope. Shared by every
// adapter so mapping and logging live in one place.
func broadcast(hub *Hub, now func() time.Time, userID, domainType string, payload any) {
	wire, ok := wireTypes[domainType]
	if !ok {
		log.Error().Str("event", domainType).Msg("ws: unmapped domain event type — dropped")
		return
	}
	hub.BroadcastToUser(userID, Event{Event: wire, Payload: payload, Timestamp: now().UTC()})
}

// TaskBroadcaster adapts the Hub to task.Broadcaster.
type TaskBroadcaster struct {
	hub *Hub
	now func() time.Time
}

func NewTaskBroadcaster(hub *Hub) *TaskBroadcaster {
	return &TaskBroadcaster{hub: hub, now: time.Now}
}

func (b *TaskBroadcaster) Broadcast(userID string, ev task.Event) {
	broadcast(b.hub, b.now, userID, ev.Type, ev.Payload)
}

// ProjectBroadcaster adapts the Hub to project.Broadcaster.
type ProjectBroadcaster struct {
	hub *Hub
	now func() time.Time
}

func NewProjectBroadcaster(hub *Hub) *ProjectBroadcaster {
	return &ProjectBroadcaster{hub: hub, now: time.Now}
}

func (b *ProjectBroadcaster) Broadcast(userID string, ev project.Event) {
	broadcast(b.hub, b.now, userID, ev.Type, ev.Payload)
}

// AreaBroadcaster adapts the Hub to area.Broadcaster.
type AreaBroadcaster struct {
	hub *Hub
	now func() time.Time
}

func NewAreaBroadcaster(hub *Hub) *AreaBroadcaster {
	return &AreaBroadcaster{hub: hub, now: time.Now}
}

func (b *AreaBroadcaster) Broadcast(userID string, ev area.Event) {
	broadcast(b.hub, b.now, userID, ev.Type, ev.Payload)
}

// BucketBroadcaster adapts the Hub to bucket.Broadcaster. The bucket domain also
// emits task.created on the process→task path; the map routes it to the task
// wire event.
type BucketBroadcaster struct {
	hub *Hub
	now func() time.Time
}

func NewBucketBroadcaster(hub *Hub) *BucketBroadcaster {
	return &BucketBroadcaster{hub: hub, now: time.Now}
}

func (b *BucketBroadcaster) Broadcast(userID string, ev bucket.Event) {
	broadcast(b.hub, b.now, userID, ev.Type, ev.Payload)
}

// NotificationBroadcaster adapts the Hub to notification.Broadcaster. It maps
// the event's Type through the shared table rather than hardcoding a single
// constant, so future notification event types route correctly.
type NotificationBroadcaster struct {
	hub *Hub
	now func() time.Time
}

// NewNotificationBroadcaster wires the adapter to a Hub. Inject it into
// notification.NewService in place of the nil seam to light up instant delivery.
func NewNotificationBroadcaster(hub *Hub) *NotificationBroadcaster {
	return &NotificationBroadcaster{hub: hub, now: time.Now}
}

func (b *NotificationBroadcaster) Broadcast(userID string, event notification.Event) {
	broadcast(b.hub, b.now, userID, event.Type, event.Payload)
}

// AttachmentBroadcaster adapts the Hub to attachment.Broadcaster.
type AttachmentBroadcaster struct {
	hub *Hub
	now func() time.Time
}

func NewAttachmentBroadcaster(hub *Hub) *AttachmentBroadcaster {
	return &AttachmentBroadcaster{hub: hub, now: time.Now}
}

func (b *AttachmentBroadcaster) Broadcast(userID string, ev attachment.Event) {
	broadcast(b.hub, b.now, userID, ev.Type, ev.Payload)
}

// RecurrenceBroadcaster adapts the Hub to recurrence.Broadcaster.
type RecurrenceBroadcaster struct {
	hub *Hub
	now func() time.Time
}

func NewRecurrenceBroadcaster(hub *Hub) *RecurrenceBroadcaster {
	return &RecurrenceBroadcaster{hub: hub, now: time.Now}
}

func (b *RecurrenceBroadcaster) Broadcast(userID string, ev recurrence.Event) {
	broadcast(b.hub, b.now, userID, ev.Type, ev.Payload)
}

// FocusBroadcaster adapts the Hub to focus.Broadcaster.
type FocusBroadcaster struct {
	hub *Hub
	now func() time.Time
}

func NewFocusBroadcaster(hub *Hub) *FocusBroadcaster {
	return &FocusBroadcaster{hub: hub, now: time.Now}
}

func (b *FocusBroadcaster) Broadcast(userID string, ev focus.Event) {
	broadcast(b.hub, b.now, userID, ev.Type, ev.Payload)
}

// NoteBroadcaster adapts the Hub to note.Broadcaster.
type NoteBroadcaster struct {
	hub *Hub
	now func() time.Time
}

func NewNoteBroadcaster(hub *Hub) *NoteBroadcaster {
	return &NoteBroadcaster{hub: hub, now: time.Now}
}

func (b *NoteBroadcaster) Broadcast(userID string, ev note.Event) {
	broadcast(b.hub, b.now, userID, ev.Type, ev.Payload)
}

// HabitBroadcaster adapts the Hub to habit.Broadcaster.
type HabitBroadcaster struct {
	hub *Hub
	now func() time.Time
}

func NewHabitBroadcaster(hub *Hub) *HabitBroadcaster {
	return &HabitBroadcaster{hub: hub, now: time.Now}
}

func (b *HabitBroadcaster) Broadcast(userID string, ev habit.Event) {
	broadcast(b.hub, b.now, userID, ev.Type, ev.Payload)
}

// AIBroadcaster adapts the Hub to ai.Broadcaster.
type AIBroadcaster struct {
	hub *Hub
	now func() time.Time
}

func NewAIBroadcaster(hub *Hub) *AIBroadcaster {
	return &AIBroadcaster{hub: hub, now: time.Now}
}

func (b *AIBroadcaster) Broadcast(userID string, ev ai.Event) {
	broadcast(b.hub, b.now, userID, ev.Type, ev.Payload)
}
