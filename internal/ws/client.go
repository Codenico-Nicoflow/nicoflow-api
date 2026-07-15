package ws

import (
	"time"

	"github.com/gorilla/websocket"
)

// Heartbeat + limit parameters (SPEC §3.6 / §3.14).
const (
	pingInterval   = 30 * time.Second // server → client ping cadence
	writeWait      = 10 * time.Second // deadline for a single write
	pongWait       = 60 * time.Second // no pong within this ⇒ dead connection
	maxMessageSize = 512              // client → server frames are tiny (we only receive pongs)
	sendBuffer     = 256              // per-client outbound queue; full ⇒ slow client dropped
)

// Client is one live WebSocket connection for a user. A user may hold several
// (multiple tabs/devices) — the Hub keys a set of clients per userID.
type Client struct {
	hub    *Hub
	userID string
	conn   *websocket.Conn
	send   chan []byte
}

// NewClient wraps an upgraded connection. Register it with the hub, then run
// ReadPump and WritePump (each in its own goroutine — gorilla requires exactly
// one concurrent reader and one concurrent writer per connection).
func NewClient(hub *Hub, userID string, conn *websocket.Conn) *Client {
	return &Client{hub: hub, userID: userID, conn: conn, send: make(chan []byte, sendBuffer)}
}

// ReadPump services the connection's single reader: clients are receive-only, so
// it discards any inbound frame and exists only to process control frames (pong)
// and detect disconnects. A missed pong trips the read deadline and ends the pump.
func (c *Client) ReadPump() {
	defer func() {
		c.hub.Unregister(c.userID, c)
		_ = c.conn.Close()
	}()

	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

// WritePump services the connection's single writer: it drains the send channel
// and fires a ping every pingInterval. A closed send channel (hub dropped us) or
// any write error ends the pump and closes the connection.
func (c *Client) WritePump() {
	ticker := time.NewTicker(pingInterval)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// Hub closed the channel — send a clean close frame and stop.
				_ = c.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
