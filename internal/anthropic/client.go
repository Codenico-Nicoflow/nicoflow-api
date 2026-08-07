// Package anthropic is a thin streaming wrapper over anthropic-sdk-go that
// implements the ai.Client interface (defined in the consumer package). It maps
// provider failures to the AI apperror codes and is the only place that imports
// the vendor SDK, so the rest of the codebase stays provider-agnostic and
// mockable — CI never makes a real API call.
package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/ai"
)

// defaultMaxTokens caps a completion when the request doesn't specify one.
const defaultMaxTokens = 1024

// Client wraps the Anthropic Messages API. A zero-value / disabled Client
// (Enabled()==false, apiKey empty) is safe to hold; Stream returns
// AI_UNAVAILABLE — the kill switch decides the 503 at the request boundary, so
// boot never fails just because the key isn't set.
type Client struct {
	api     *sdk.Client
	enabled bool
}

// New builds the client from the API key. An empty key returns a disabled
// client (nil error), mirroring internal/storage.
func New(apiKey string) *Client {
	if apiKey == "" {
		return &Client{}
	}
	c := sdk.NewClient(option.WithAPIKey(apiKey))
	return &Client{api: &c, enabled: true}
}

// Enabled reports whether a provider key is configured.
func (c *Client) Enabled() bool { return c.enabled }

// Stream starts a streaming completion.
func (c *Client) Stream(ctx context.Context, req ai.ChatRequest) (ai.Stream, error) {
	if !c.enabled {
		return nil, apperror.New(http.StatusServiceUnavailable, apperror.ErrAIUnavailable, "ai assistant is not available")
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
	}

	msgs, err := toSDKMessages(req.Messages)
	if err != nil {
		return nil, apperror.New(http.StatusInternalServerError, apperror.ErrInternalServerError, "failed to build request messages")
	}

	params := sdk.MessageNewParams{
		Model:     sdk.Model(req.Model),
		MaxTokens: int64(maxTokens),
		Messages:  msgs,
	}
	if req.System != "" {
		sys := sdk.TextBlockParam{Text: req.System}
		if req.CacheSystem {
			sys.CacheControl = ephemeral()
		}
		params.System = []sdk.TextBlockParam{sys}
	}
	if len(req.Tools) > 0 {
		tools, terr := toSDKTools(req.Tools)
		if terr != nil {
			return nil, apperror.New(http.StatusInternalServerError, apperror.ErrInternalServerError, "invalid tool schema")
		}
		params.Tools = tools
	}

	return &stream{raw: c.api.Messages.NewStreaming(ctx, params)}, nil
}

// ephemeral is the 5-minute cache-control breakpoint marker.
func ephemeral() sdk.CacheControlEphemeralParam {
	return sdk.CacheControlEphemeralParam{}
}

// toSDKTools translates the domain-level tool catalog into the SDK's tool union.
// The InputSchema is a raw JSON schema object; we unmarshal it into the SDK's
// ToolInputSchemaParam so its `type: object` + `properties` + `required` fields
// land where the SDK expects them and the wire payload stays canonical.
func toSDKTools(defs []ai.ToolDefinition) ([]sdk.ToolUnionParam, error) {
	out := make([]sdk.ToolUnionParam, len(defs))
	for i, d := range defs {
		var schema sdk.ToolInputSchemaParam
		if len(d.InputSchema) > 0 {
			if err := json.Unmarshal(d.InputSchema, &schema); err != nil {
				return nil, err
			}
		}
		tool := sdk.ToolParam{
			Name:        d.Name,
			InputSchema: schema,
		}
		if d.Description != "" {
			tool.Description = sdk.String(d.Description)
		}
		out[i] = sdk.ToolUnionParam{OfTool: &tool}
	}
	return out, nil
}

// toSDKMessages rebuilds the wire history from the domain messages, including
// any prior tool_use (assistant) and tool_result (user) blocks so multi-turn
// tool loops preserve their round-trip context.
func toSDKMessages(msgs []ai.Message) ([]sdk.MessageParam, error) {
	out := make([]sdk.MessageParam, 0, len(msgs))
	for _, m := range msgs {
		converted, err := oneMessage(m)
		if err != nil {
			return nil, err
		}
		if converted != nil {
			out = append(out, *converted)
		}
	}
	return out, nil
}

// oneMessage translates one domain message into an SDK MessageParam. Returns
// nil (no error) for an empty message the SDK would reject.
func oneMessage(m ai.Message) (*sdk.MessageParam, error) {
	if m.Role == ai.RoleAssistant {
		blocks, err := assistantBlocks(m)
		if err != nil || len(blocks) == 0 {
			return nil, err
		}
		msg := sdk.NewAssistantMessage(blocks...)
		return &msg, nil
	}
	blocks := userBlocks(m)
	if len(blocks) == 0 {
		return nil, nil
	}
	msg := sdk.NewUserMessage(blocks...)
	return &msg, nil
}

func assistantBlocks(m ai.Message) ([]sdk.ContentBlockParamUnion, error) {
	blocks := make([]sdk.ContentBlockParamUnion, 0, 1+len(m.ToolUses))
	if m.Text != "" {
		text := sdk.TextBlockParam{Text: m.Text}
		if m.CacheBreakpoint && len(m.ToolUses) == 0 {
			text.CacheControl = ephemeral()
		}
		blocks = append(blocks, sdk.ContentBlockParamUnion{OfText: &text})
	}
	for i, tu := range m.ToolUses {
		var input any
		if len(tu.Input) > 0 {
			if err := json.Unmarshal(tu.Input, &input); err != nil {
				return nil, err
			}
		}
		block := sdk.NewToolUseBlock(tu.ID, input, tu.Name)
		if m.CacheBreakpoint && i == len(m.ToolUses)-1 && block.OfToolUse != nil {
			block.OfToolUse.CacheControl = ephemeral()
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func userBlocks(m ai.Message) []sdk.ContentBlockParamUnion {
	blocks := make([]sdk.ContentBlockParamUnion, 0, 1+len(m.ToolResults))
	if m.Text != "" {
		text := sdk.TextBlockParam{Text: m.Text}
		if m.CacheBreakpoint && len(m.ToolResults) == 0 {
			text.CacheControl = ephemeral()
		}
		blocks = append(blocks, sdk.ContentBlockParamUnion{OfText: &text})
	}
	for _, tr := range m.ToolResults {
		blocks = append(blocks, sdk.NewToolResultBlock(tr.ToolUseID, tr.Content, tr.IsError))
	}
	return blocks
}

// stream adapts the SDK's SSE stream to ai.Stream. It accumulates the message
// via the SDK helper so both Usage and the final content blocks (text +
// tool_use) are available after the stream drains.
type stream struct {
	raw     *ssestream.Stream[sdk.MessageStreamEventUnion]
	acc     sdk.Message
	text    string
	err     error
	drained bool
}

func (s *stream) Next() bool {
	if s.err != nil {
		return false
	}
	for s.raw.Next() {
		event := s.raw.Current()
		if err := s.acc.Accumulate(event); err != nil {
			s.err = apperror.New(http.StatusBadGateway, apperror.ErrAIProviderError, "ai provider stream error")
			return false
		}
		if delta, ok := event.AsAny().(sdk.ContentBlockDeltaEvent); ok && delta.Delta.Text != "" {
			s.text = delta.Delta.Text
			return true
		}
	}
	s.drained = true
	if err := s.raw.Err(); err != nil {
		s.err = mapProviderErr(err)
	}
	return false
}

func (s *stream) Text() string { return s.text }

func (s *stream) Err() error { return s.err }

func (s *stream) Usage() ai.Usage {
	return ai.Usage{
		InputTokens:  s.acc.Usage.InputTokens,
		OutputTokens: s.acc.Usage.OutputTokens,
	}
}

// ToolUses extracts every tool_use content block from the accumulated message.
// The SDK's ToolUseBlock.Input is already the raw JSON bytes emitted by the
// model — we forward those verbatim rather than re-marshal, so the executor
// sees exactly what the provider produced.
func (s *stream) ToolUses() []ai.ToolUseBlock {
	if !s.drained {
		return nil
	}
	var out []ai.ToolUseBlock
	for _, block := range s.acc.Content {
		if variant, ok := block.AsAny().(sdk.ToolUseBlock); ok {
			out = append(out, ai.ToolUseBlock{
				ID:    variant.ID,
				Name:  variant.Name,
				Input: append(json.RawMessage(nil), variant.Input...),
			})
		}
	}
	return out
}

func (s *stream) StopReason() string {
	if !s.drained {
		return ""
	}
	return string(s.acc.StopReason)
}

func (s *stream) Close() error { return s.raw.Close() }

// mapProviderErr translates a provider HTTP error into an AI apperror code:
// 429/529 (rate-limit / overloaded) and timeouts ⇒ AI_UNAVAILABLE (503, retry
// later); everything else (400/401/5xx) ⇒ AI_PROVIDER_ERROR (502, our fault).
func mapProviderErr(err error) *apperror.AppError {
	var apiErr *sdk.Error
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case http.StatusTooManyRequests, 529:
			return apperror.New(http.StatusServiceUnavailable, apperror.ErrAIUnavailable, "ai assistant is temporarily unavailable")
		}
	}
	return apperror.New(http.StatusBadGateway, apperror.ErrAIProviderError, "ai provider error")
}
