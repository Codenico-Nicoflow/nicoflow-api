package ws

import (
	"time"

	"github.com/gin-gonic/gin"
)

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
)

// Upgrader is set in main to avoid importing gorilla/websocket here if not yet available.
// Client holds the websocket connection for one user.
type Client struct {
	hub    *Hub
	userID string
	conn   interface{ WriteMessage(int, []byte) error }
	send   chan []byte
}

func NewClient(hub *Hub, userID string, conn interface{ WriteMessage(int, []byte) error }, c *gin.Context) *Client {
	_ = c // gin context reserved for future use
	return &Client{hub: hub, userID: userID, conn: conn, send: make(chan []byte, 256)}
}

func (c *Client) WritePump() {
	for msg := range c.send {
		_ = c.conn.WriteMessage(1 /*websocket.TextMessage*/, msg)
	}
}
