package ws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

// AC2: the adapter maps a notification.Event onto a ws.Event with event
// "notification.created", the same payload, and a UTC timestamp, and delivers it
// to the user's live connections.
func TestNotificationBroadcaster_MapsAndDelivers(t *testing.T) {
	h := NewHub()
	c := newTestClient("u1")
	h.Register("u1", c)

	fixed := time.Date(2026, 7, 16, 8, 0, 0, 0, time.FixedZone("x", 3*3600))
	b := NewNotificationBroadcaster(h)
	b.now = func() time.Time { return fixed }

	b.Broadcast("u1", notification.Event{
		Type:    "notification.created",
		Payload: notification.NotificationView{ID: "n1", Type: "task_due_soon", Title: "Task due"},
	})

	select {
	case msg := <-c.send:
		var ev Event
		if err := json.Unmarshal(msg, &ev); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if ev.Event != EventNotificationCreated {
			t.Errorf("event = %q, want %q", ev.Event, EventNotificationCreated)
		}
		if !ev.Timestamp.Equal(fixed) {
			t.Errorf("timestamp = %v, want %v", ev.Timestamp, fixed)
		}
		if ev.Timestamp.Location() != time.UTC {
			t.Errorf("timestamp location = %v, want UTC", ev.Timestamp.Location())
		}
	default:
		t.Fatal("expected a delivered frame, got none")
	}
}

// A broadcast for a user with no live connection is a silent no-op (fire-and-forget).
func TestNotificationBroadcaster_NoConnectionIsNoop(t *testing.T) {
	b := NewNotificationBroadcaster(NewHub())
	b.Broadcast("ghost", notification.Event{Type: "notification.created"})
}
