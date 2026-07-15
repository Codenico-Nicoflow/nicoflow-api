package ws

import (
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/nicoflow/nicoflow-api/pkg/jwtutil"
)

// Handler upgrades HTTP requests to WebSocket connections. WS is available on all
// plans (free) — the JWT is validated only for identity, not plan.
type Handler struct {
	hub       *Hub
	jwtSecret string
	upgrader  websocket.Upgrader
}

// NewHandler builds the WS handler. allowedOrigins is the CORS origin allowlist
// (comma-separated); an empty list rejects all cross-origin upgrades.
func NewHandler(hub *Hub, jwtSecret, allowedOrigins string) *Handler {
	origins := splitOrigins(allowedOrigins)
	return &Handler{
		hub:       hub,
		jwtSecret: jwtSecret,
		upgrader: websocket.Upgrader{
			HandshakeTimeout: writeWait,
			CheckOrigin: func(r *http.Request) bool {
				origin := r.Header.Get("Origin")
				if origin == "" {
					// Non-browser client (no Origin header) — allow; JWT still gates identity.
					return true
				}
				return slices.Contains(origins, origin)
			},
		},
	}
}

// Upgrade authenticates via the ?token= query param (browsers can't set headers on
// the WS handshake), upgrades the connection, registers it, and starts the pumps.
// An invalid/expired token upgrades then closes with 1008 Policy Violation.
func (h *Handler) Upgrade(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")

	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		// Upgrade already wrote an error response; nothing more to do.
		return
	}

	claims, err := jwtutil.Parse(token, h.jwtSecret)
	if err != nil {
		_ = conn.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "invalid token"),
			time.Now().Add(writeWait),
		)
		_ = conn.Close()
		return
	}

	client := NewClient(h.hub, claims.Subject, conn)
	h.hub.Register(claims.Subject, client)
	go client.WritePump()
	go client.ReadPump()
}

func splitOrigins(csv string) []string {
	if csv == "" {
		return nil
	}
	parts := strings.Split(csv, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
