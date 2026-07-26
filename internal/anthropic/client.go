// Package anthropic is a thin streaming wrapper over anthropic-sdk-go that
// implements the ai.Client interface (defined in the consumer package). It maps
// provider failures to the AI apperror codes and is the only place that imports
// the vendor SDK, so the rest of the codebase stays provider-agnostic and
// mockable — CI never makes a real API call.
package anthropic

import (
	"context"
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

	params := sdk.MessageNewParams{
		Model:     sdk.Model(req.Model),
		MaxTokens: int64(maxTokens),
		Messages:  toSDKMessages(req.Messages),
	}
	if req.System != "" {
		params.System = []sdk.TextBlockParam{{Text: req.System}}
	}

	return &stream{raw: c.api.Messages.NewStreaming(ctx, params)}, nil
}

func toSDKMessages(msgs []ai.Message) []sdk.MessageParam {
	out := make([]sdk.MessageParam, 0, len(msgs))
	for _, m := range msgs {
		block := sdk.NewTextBlock(m.Text)
		if m.Role == ai.RoleAssistant {
			out = append(out, sdk.NewAssistantMessage(block))
			continue
		}
		out = append(out, sdk.NewUserMessage(block))
	}
	return out
}

// stream adapts the SDK's SSE stream to ai.Stream, accumulating usage as it
// drains so Usage() is final once Next reports the stream is done.
type stream struct {
	raw  *ssestream.Stream[sdk.MessageStreamEventUnion]
	acc  sdk.Message
	text string
	err  error
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
