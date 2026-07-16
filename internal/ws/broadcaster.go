package ws

import (
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

// NotificationBroadcaster adapts the live Hub to notification.Broadcaster, so the
// notification service (which knows nothing about WebSockets) can emit its
// full-payload Event and have it delivered over WS. The adapter lives here, in
// ws, importing notification — never the reverse — keeping the domain package
// transport-agnostic.
type NotificationBroadcaster struct {
	hub *Hub
	now func() time.Time
}

// NewNotificationBroadcaster wires the adapter to a Hub. Inject it into
// notification.NewService in place of the nil seam to light up instant delivery.
func NewNotificationBroadcaster(hub *Hub) *NotificationBroadcaster {
	return &NotificationBroadcaster{hub: hub, now: time.Now}
}

// Broadcast maps a notification.Event onto the WS envelope and fans it out to the
// user's connections. Fire-and-forget: BroadcastToUser silently skips a user with
// no live connection, so a broadcast never blocks or fails the Create path.
func (b *NotificationBroadcaster) Broadcast(userID string, event notification.Event) {
	b.hub.BroadcastToUser(userID, Event{
		Event:     EventNotificationCreated,
		Payload:   event.Payload,
		Timestamp: b.now().UTC(),
	})
}
