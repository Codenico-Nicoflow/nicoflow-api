package ws

import "sync"

type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client // keyed by userID
}

func NewHub() *Hub {
	return &Hub{clients: make(map[string]*Client)}
}

func (h *Hub) Register(userID string, c *Client) {
	h.mu.Lock()
	h.clients[userID] = c
	h.mu.Unlock()
}

func (h *Hub) Unregister(userID string) {
	h.mu.Lock()
	delete(h.clients, userID)
	h.mu.Unlock()
}

func (h *Hub) Broadcast(userID string, msg []byte) {
	h.mu.RLock()
	c, ok := h.clients[userID]
	h.mu.RUnlock()
	if ok {
		c.send <- msg
	}
}
