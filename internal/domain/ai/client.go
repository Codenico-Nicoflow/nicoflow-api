package ai

import "context"

// Role identifies who authored a chat turn. Only user/assistant cross the wire;
// the system prompt is a separate field on ChatRequest, not a message.
type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one turn of a conversation sent to the model. CacheBreakpoint marks
// this block as the last cache-control breakpoint (ephemeral) — set on the final
// history message so the prefix up to it is reused across turns.
type Message struct {
	Role            Role
	Text            string
	CacheBreakpoint bool
}

// ChatRequest is a provider-agnostic streaming chat call. Model is resolved by
// the service from config (never hardcoded); System is optional. CacheSystem
// marks the system block as an ephemeral cache breakpoint.
type ChatRequest struct {
	Model       string
	System      string
	CacheSystem bool
	Messages    []Message
	MaxTokens   int
}

// Usage is the token accounting for a completed stream.
type Usage struct {
	InputTokens  int64
	OutputTokens int64
}

// Client is the streaming AI provider seam. It lives in the ai (consumer)
// package per the project's interface-ownership rule; internal/anthropic
// implements it and CI mocks it, so tests make no real API calls.
type Client interface {
	// Enabled reports whether a provider key is configured. Disabled ⇒ the
	// kill switch returns 503 AI_UNAVAILABLE before any handler logic.
	Enabled() bool
	// Stream starts a streaming completion. It returns a Stream the caller
	// drains, or a *apperror.AppError (AI_UNAVAILABLE / AI_PROVIDER_ERROR)
	// when the request can't be started.
	Stream(ctx context.Context, req ChatRequest) (Stream, error)
}

// Stream is a forward-only cursor over a completion's text deltas. Usage is
// valid only after Next returns false with no error. Callers must Close.
//
//	for s.Next() { io.WriteString(w, s.Text()) }
//	if err := s.Err(); err != nil { ... }
type Stream interface {
	Next() bool
	Text() string
	Err() error
	Usage() Usage
	Close() error
}
