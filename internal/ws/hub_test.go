package ws

import (
	"encoding/json"
	"testing"
	"time"
)

// newTestClient builds a Client with just a send channel — enough to exercise the
// hub's registration + fan-out without a real network connection.
func newTestClient(userID string) *Client {
	return &Client{userID: userID, send: make(chan []byte, sendBuffer)}
}

func TestHub_BroadcastToUser_FansOutToAllConnections(t *testing.T) {
	h := NewHub()
	c1 := newTestClient("u1")
	c2 := newTestClient("u1") // same user, second tab
	other := newTestClient("u2")
	h.Register("u1", c1)
	h.Register("u1", c2)
	h.Register("u2", other)

	h.BroadcastToUser("u1", Event{Event: EventNotificationCreated, Payload: map[string]string{"id": "n1"}, Timestamp: time.Now()})

	for i, c := range []*Client{c1, c2} {
		select {
		case msg := <-c.send:
			var ev Event
			if err := json.Unmarshal(msg, &ev); err != nil {
				t.Fatalf("client %d: unmarshal: %v", i, err)
			}
			if ev.Event != EventNotificationCreated {
				t.Errorf("client %d: event = %q, want %q", i, ev.Event, EventNotificationCreated)
			}
		default:
			t.Errorf("client %d: expected a message, got none", i)
		}
	}

	// The other user must not receive u1's event.
	select {
	case <-other.send:
		t.Error("u2 received u1's event — cross-user leak")
	default:
	}
}

func TestHub_BroadcastToUser_NoConnectionIsNoop(t *testing.T) {
	h := NewHub()
	// Must not panic when the user has no live connection.
	h.BroadcastToUser("ghost", Event{Event: EventTaskUpdated, Timestamp: time.Now()})
}

func TestHub_Unregister_RemovesAndClosesSend(t *testing.T) {
	h := NewHub()
	c := newTestClient("u1")
	h.Register("u1", c)
	h.Unregister("u1", c)

	// send must be closed so the client's WritePump exits.
	if _, ok := <-c.send; ok {
		t.Error("send channel not closed after Unregister")
	}

	// A second Unregister is a harmless no-op (no double-close panic).
	h.Unregister("u1", c)

	// The empty user set is pruned → broadcast is a no-op, not a nil-map write.
	h.BroadcastToUser("u1", Event{Event: EventTaskUpdated, Timestamp: time.Now()})
}

func TestHub_BroadcastToUser_DropsSlowClient(t *testing.T) {
	h := NewHub()
	c := newTestClient("u1")
	h.Register("u1", c)

	// Fill the send buffer so the next broadcast can't enqueue.
	for range sendBuffer {
		c.send <- []byte("x")
	}

	// This broadcast finds the buffer full → drops (unregisters) the slow client.
	h.BroadcastToUser("u1", Event{Event: EventTaskUpdated, Timestamp: time.Now()})

	h.mu.RLock()
	_, stillRegistered := h.clients["u1"]
	h.mu.RUnlock()
	if stillRegistered {
		t.Error("slow client was not dropped")
	}
}
