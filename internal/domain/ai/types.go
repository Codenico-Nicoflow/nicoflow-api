// Package ai contains the AI assistant domain.
package ai

import (
	"encoding/json"
	"time"
)

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
//
// ContentJSON is the tool-aware companion column (migration 046): when set,
// it holds the ordered blocks (text + tool_use for assistant, tool_result for
// user) exactly as Claude sees them. Nil ⇒ pre-tool-loop turn where Content
// alone is the whole message.
type SessionMessage struct {
	ID          string
	SessionID   string
	Role        string
	Content     string
	ContentJSON json.RawMessage
	CreatedAt   time.Time
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

// SendMessageRequest is the POST /ai/sessions/:id/messages body. Content is
// trimmed then length-validated (1..2000) before any provider call.
type SendMessageRequest struct {
	Content string `json:"content"`
}

// PromptContext is the volatile, per-user tail of the system prompt.
type PromptContext struct {
	Language  string
	OpenTasks int
}

// SSE event payloads streamed over the response body (type-discriminated).
type (
	// deltaEvent carries one text delta.
	deltaEvent struct {
		Type string `json:"type"` // "delta"
		Text string `json:"text"`
	}
	// doneEvent terminates a successful stream with the persisted id + usage.
	doneEvent struct {
		Type      string    `json:"type"` // "done"
		MessageID string    `json:"messageId"`
		Usage     UsageView `json:"usage"`
	}
	// errorEvent terminates a mid-stream failure (HTTP status already committed).
	errorEvent struct {
		Type string `json:"type"` // "error"
		Code string `json:"code"`
	}
	// toolProposalEvent surfaces one pending write-tool proposal to the client
	// — the confirm/reject UI card is driven off this. Input is passed through
	// verbatim from the model.
	toolProposalEvent struct {
		Type               string          `json:"type"` // "tool_proposal"
		AssistantMessageID string          `json:"assistantMessageId"`
		ToolUseID          string          `json:"toolUseId"`
		ToolName           string          `json:"toolName"`
		Input              json.RawMessage `json:"input"`
	}
)

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
