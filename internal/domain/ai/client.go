package ai

import (
	"context"
	"encoding/json"
)

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
//
// A tool round-trip is expressed by ToolUses (on an assistant turn) and
// ToolResults (on a user turn). Text may still be set alongside ToolUses when the
// assistant produces both narration and one or more tool_use blocks in the same
// turn — the provider adapter preserves the ordering.
type Message struct {
	Role            Role
	Text            string
	CacheBreakpoint bool
	// ToolUses is set on RoleAssistant turns that carried tool_use blocks. The
	// provider adapter round-trips these back to the model as part of the
	// assistant message; the ai domain never fabricates them.
	ToolUses []ToolUseBlock
	// ToolResults is set on RoleUser turns that carry tool_result blocks. Each
	// entry is a completed executor result (or an is_error result) paired to a
	// prior ToolUseBlock.ID from the assistant turn immediately before this one.
	ToolResults []ToolResultBlock
}

// ToolUseBlock is the domain-level shape of one Claude tool_use content block.
// Input is the raw JSON of the tool arguments as the model emitted it — the
// executor unmarshals it into the tool's typed input.
type ToolUseBlock struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResultBlock is the response we hand back to Claude for a prior ToolUseBlock.
// Content is the JSON-encoded executor result (or an error envelope when IsError).
type ToolResultBlock struct {
	ToolUseID string
	Content   string
	IsError   bool
}

// ChatRequest is a provider-agnostic streaming chat call. Model is resolved by
// the service from config (never hardcoded); System is optional. CacheSystem
// marks the system block as an ephemeral cache breakpoint. Tools is the tool
// catalog the model may call this turn — nil disables tool_use entirely.
type ChatRequest struct {
	Model       string
	System      string
	CacheSystem bool
	Messages    []Message
	MaxTokens   int
	Tools       []ToolDefinition
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

// Stream is a forward-only cursor over a completion's text deltas. Usage,
// ToolUses, and StopReason are valid only after Next returns false with no
// error. Callers must Close.
//
//	for s.Next() { io.WriteString(w, s.Text()) }
//	if err := s.Err(); err != nil { ... }
//	for _, tu := range s.ToolUses() { ... }
type Stream interface {
	Next() bool
	Text() string
	Err() error
	Usage() Usage
	// ToolUses returns the tool_use blocks the assistant emitted this turn, in
	// document order. Valid only after Next reports done with no error.
	ToolUses() []ToolUseBlock
	// StopReason is the terminal stop_reason from the provider ("end_turn",
	// "tool_use", "max_tokens", …). Valid only after Next reports done.
	StopReason() string
	Close() error
}
