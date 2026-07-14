package notification

import (
	"encoding/json"
	"time"
)

// Type values for a notification.
const (
	TypeTaskDueSoon        = "task_due_soon"
	TypeSystemAnnouncement = "system_announcement"
)

// Notification is the internal domain model.
type Notification struct {
	ID        string
	UserID    string
	Type      string
	Title     string
	Body      string
	Metadata  json.RawMessage
	IsRead    bool
	ReadAt    *time.Time
	DedupeKey *string
	CreatedAt time.Time
}

// NotificationView is the JSON response shape for a single notification.
type NotificationView struct {
	ID        string          `json:"id"`
	Type      string          `json:"type"`
	Title     string          `json:"title"`
	Body      string          `json:"body"`
	Metadata  json.RawMessage `json:"metadata" swaggertype:"object"`
	IsRead    bool            `json:"isRead"`
	ReadAt    *string         `json:"readAt"`
	CreatedAt string          `json:"createdAt"`
}

// ListNotificationsResponse is the cursor-paginated list response.
type ListNotificationsResponse struct {
	Items      []NotificationView `json:"items"`
	NextCursor string             `json:"nextCursor"`
}

// UnreadCountResponse is the payload of the unread-count endpoint.
type UnreadCountResponse struct {
	Count int `json:"count"`
}

// CountResponse is the payload of bulk mutations (e.g. mark-all-read).
type CountResponse struct {
	Count int `json:"count"`
}

// ListNotificationsFilter holds parsed query parameters for the list endpoint.
type ListNotificationsFilter struct {
	IsRead *bool
	Limit  int
	Cursor string
}

// Event is the full-payload shape broadcast to a user's WebSocket connections
// when a notification is created. Built now so E-022 can inject a real hub as
// the Broadcaster without changing the service (WS-ready seam).
type Event struct {
	Type    string           `json:"type"`
	Payload NotificationView `json:"payload"`
}

func notificationToView(n Notification) NotificationView {
	var readAt *string
	if n.ReadAt != nil {
		s := n.ReadAt.UTC().Format(time.RFC3339)
		readAt = &s
	}
	meta := n.Metadata
	if len(meta) == 0 {
		meta = json.RawMessage(`{}`)
	}
	return NotificationView{
		ID:        n.ID,
		Type:      n.Type,
		Title:     n.Title,
		Body:      n.Body,
		Metadata:  meta,
		IsRead:    n.IsRead,
		ReadAt:    readAt,
		CreatedAt: n.CreatedAt.UTC().Format(time.RFC3339),
	}
}
