package ws

import (
	"encoding/json"
	"sync"

	"github.com/rs/zerolog/log"
)

// Hub tracks every live connection, keyed by userID. A user may have multiple
// connections (tabs/devices); events fan out to all of them. In-process only —
// no Redis in v1 (single Render instance). All operations are goroutine-safe.
type Hub struct {
	mu      sync.RWMutex
	clients map[string]map[*Client]struct{}
}

func NewHub() *Hub {
	return &Hub{clients: make(map[string]map[*Client]struct{})}
}

// Register adds a client to its user's connection set.
func (h *Hub) Register(userID string, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.clients[userID]
	if set == nil {
		set = make(map[*Client]struct{})
		h.clients[userID] = set
	}
	set[c] = struct{}{}
}

// Unregister removes a client and closes its send channel so its WritePump exits.
// Idempotent: unregistering an already-removed client is a no-op.
func (h *Hub) Unregister(userID string, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	set := h.clients[userID]
	if set == nil {
		return
	}
	if _, ok := set[c]; !ok {
		return
	}
	delete(set, c)
	close(c.send)
	if len(set) == 0 {
		delete(h.clients, userID)
	}
}

// BroadcastToUser marshals the event once and delivers it to every connection the
// user has. A client whose send buffer is full is a slow/stuck consumer: we drop
// it (non-blocking send) so one bad connection can never stall the hub. Delivery
// is best-effort — a user with no live connection is silently skipped.
func (h *Hub) BroadcastToUser(userID string, ev Event) {
	msg, err := json.Marshal(ev)
	if err != nil {
		log.Error().Err(err).Str("event", string(ev.Event)).Msg("ws: marshal event failed")
		return
	}

	h.mu.RLock()
	set := h.clients[userID]
	targets := make([]*Client, 0, len(set))
	for c := range set {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	for _, c := range targets {
		select {
		case c.send <- msg:
		default:
			// Buffer full → slow consumer. Drop it; ReadPump/WritePump cleanup follows.
			h.Unregister(userID, c)
		}
	}
}

// ClientCount reports how many live connections a user currently holds. Used by
// tests to wait out the register race before broadcasting.
func (h *Hub) ClientCount(userID string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients[userID])
}

// CloseAll closes every connection — used on graceful shutdown.
func (h *Hub) CloseAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for userID, set := range h.clients {
		for c := range set {
			close(c.send)
		}
		delete(h.clients, userID)
	}
}
