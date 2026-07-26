// Package ai contains the AI assistant domain.
package ai

import "time"

// Session is a persisted AI conversation. status/title default in migration 009.
type Session struct {
	ID        string
	UserID    string
	Title     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// SessionMessage is one persisted turn of a conversation (ai_messages, mig 010).
// Distinct from the streaming Message in client.go, which never touches the DB.
type SessionMessage struct {
	ID        string
	SessionID string
	Role      string
	Content   string
	CreatedAt time.Time
}

// SessionView is the wire shape for a session. IDs are application-generated strings.
type SessionView struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// MessageView is the wire shape for a persisted message.
type MessageView struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"createdAt"`
}

// SessionDetailView is a session plus its messages (GET /ai/sessions/:id).
type SessionDetailView struct {
	SessionView
	Messages []MessageView `json:"messages"`
}

// UsageView is the quota-state read (GET /ai/usage). For Free plans scope is
// "lifetime" and month is nil; for Pro it is "month" and month is "YYYY-MM".
type UsageView struct {
	Used  int     `json:"used"`
	Limit int     `json:"limit"`
	Scope string  `json:"scope"`
	Month *string `json:"month"`
}

// CreateSessionRequest is the POST /ai/sessions body. Title is optional;
// empty falls back to the DB default.
type CreateSessionRequest struct {
	Title string `json:"title"`
}

func sessionToView(s Session) SessionView {
	return SessionView{
		ID:        s.ID,
		Title:     s.Title,
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
	}
}

func messageToView(m SessionMessage) MessageView {
	return MessageView{
		ID:        m.ID,
		Role:      m.Role,
		Content:   m.Content,
		CreatedAt: m.CreatedAt,
	}
}
