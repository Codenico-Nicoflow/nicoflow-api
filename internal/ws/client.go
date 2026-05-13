package ws

// Client holds the WebSocket connection for one connected user.
type Client struct {
	hub    *Hub
	userID string
	conn   interface{ WriteMessage(int, []byte) error }
	send   chan []byte
}

// NewClient creates a new WebSocket client. The connection interface is satisfied
// by *gorilla/websocket.Conn — wired in the WS handler once E-022 is implemented.
func NewClient(hub *Hub, userID string, conn interface{ WriteMessage(int, []byte) error }) *Client {
	return &Client{hub: hub, userID: userID, conn: conn, send: make(chan []byte, 256)}
}

// WritePump drains the send channel and writes each message to the connection.
func (c *Client) WritePump() {
	for msg := range c.send {
		_ = c.conn.WriteMessage(1 /*websocket.TextMessage*/, msg)
	}
}
