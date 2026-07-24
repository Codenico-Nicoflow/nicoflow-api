package ws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/area"
	"github.com/nicoflow/nicoflow-api/internal/domain/bucket"
	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
	"github.com/nicoflow/nicoflow-api/internal/domain/project"
	"github.com/nicoflow/nicoflow-api/internal/domain/task"
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

// receiveType registers a probe client, runs send, and returns the wire event
// name of the frame it received ("" if none arrived).
func receiveType(t *testing.T, hub *Hub, send func()) string {
	t.Helper()
	c := newTestClient("u1")
	hub.Register("u1", c)
	defer hub.Unregister("u1", c)

	send()

	select {
	case msg := <-c.send:
		var ev Event
		if err := json.Unmarshal(msg, &ev); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return string(ev.Event)
	default:
		return ""
	}
}

// Every adapter must translate its domain event type onto the right wire event —
// mapped through the shared table, never hardcoded.
func TestAdapters_MapDomainTypeToWireType(t *testing.T) {
	hub := NewHub()

	tests := []struct {
		name string
		send func()
		want EventType
	}{
		{"task created", func() {
			NewTaskBroadcaster(hub).Broadcast("u1", task.Event{Type: task.EventCreated})
		}, EventTaskCreated},
		{"task status_changed", func() {
			NewTaskBroadcaster(hub).Broadcast("u1", task.Event{Type: task.EventStatusChanged})
		}, EventTaskStatusChanged},
		{"project updated", func() {
			NewProjectBroadcaster(hub).Broadcast("u1", project.Event{Type: project.EventUpdated})
		}, EventProjectUpdated},
		{"area deleted", func() {
			NewAreaBroadcaster(hub).Broadcast("u1", area.Event{Type: area.EventDeleted})
		}, EventAreaDeleted},
		{"bucket created", func() {
			NewBucketBroadcaster(hub).Broadcast("u1", bucket.Event{Type: bucket.EventCreated})
		}, EventBucketCreated},
		{"bucket processed", func() {
			NewBucketBroadcaster(hub).Broadcast("u1", bucket.Event{Type: bucket.EventProcessed})
		}, EventBucketProcessed},
		{"bucket deleted", func() {
			NewBucketBroadcaster(hub).Broadcast("u1", bucket.Event{Type: bucket.EventDeleted})
		}, EventBucketDeleted},
		{"bucket-emitted task.created routes to the task wire event", func() {
			NewBucketBroadcaster(hub).Broadcast("u1", bucket.Event{Type: bucket.EventTaskCreated})
		}, EventTaskCreated},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := receiveType(t, hub, tt.send); got != string(tt.want) {
				t.Errorf("wire event = %q, want %q", got, tt.want)
			}
		})
	}
}

// An unmapped domain type must be dropped (logged), never sent mangled.
func TestAdapters_UnmappedTypeIsDropped(t *testing.T) {
	hub := NewHub()
	got := receiveType(t, hub, func() {
		NewTaskBroadcaster(hub).Broadcast("u1", task.Event{Type: "task.exploded"})
	})
	if got != "" {
		t.Errorf("received %q, want no frame for an unmapped type", got)
	}
}
