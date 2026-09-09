// Package expopush wraps the Expo Push Service behind a small, fakeable surface,
// mirroring pushutil's shape for Web Push. It is a no-op when disabled, so local
// and dev environments run without any Expo credentials.
package expopush

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// SendURL is the Expo Push Service endpoint. Exposed so tests can point a client
// at a local httptest server.
const SendURL = "https://exp.host/--/api/v2/push/send"

// MaxBatchSize is Expo's documented per-request message limit.
const MaxBatchSize = 100

// Message is one push to one Expo token.
type Message struct {
	To    string         `json:"to"`
	Title string         `json:"title"`
	Body  string         `json:"body"`
	Data  map[string]any `json:"data,omitempty"`
}

// Result reports the outcome for a single token in the batch. Order matches the
// messages passed to Send — Expo returns one ticket per message.
type Result struct {
	Token string
	// Unregistered is Expo's DeviceNotRegistered: the token is dead and the
	// caller should prune it. This is the Expo analogue of Web Push's 404/410.
	Unregistered bool
	// Err is set when this individual message failed for any other reason. A
	// per-message failure never fails the whole batch.
	Err error
}

// Sender delivers a batch of messages to the Expo Push Service.
type Sender interface {
	// Send pushes msgs and returns one Result per message, in the same order.
	// A non-nil error means the whole request failed (network, non-200, or a
	// malformed response) and no per-message results are available.
	Send(ctx context.Context, msgs []Message) ([]Result, error)
}

// noop is the Sender used when Expo push is disabled: every send is a silent
// no-op reporting success, so nothing is ever pruned on a misconfigured env.
type noop struct{}

func (noop) Send(_ context.Context, msgs []Message) ([]Result, error) {
	out := make([]Result, len(msgs))
	for i, m := range msgs {
		out[i] = Result{Token: m.To}
	}
	return out, nil
}

type client struct {
	http *http.Client
	url  string
	// accessToken is optional. Expo only requires it when the project has
	// enhanced push security enabled; empty means send unauthenticated.
	accessToken string
}

// New builds a Sender. `enabled` false returns a no-op Sender (safe local/dev),
// matching how pushutil treats unset VAPID keys.
func New(enabled bool, accessToken string) Sender {
	if !enabled {
		return noop{}
	}
	return &client{
		http:        &http.Client{Timeout: 10 * time.Second},
		url:         SendURL,
		accessToken: accessToken,
	}
}

// ticket mirrors one entry of the Expo response's `data` array.
type ticket struct {
	Status  string `json:"status"`
	ID      string `json:"id"`
	Message string `json:"message"`
	Details struct {
		Error string `json:"error"`
	} `json:"details"`
}

type sendResponse struct {
	Data   []ticket `json:"data"`
	Errors []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"errors"`
}

// Send posts the batch and maps each ticket back to its token.
func (c *client) Send(ctx context.Context, msgs []Message) ([]Result, error) {
	if len(msgs) == 0 {
		return nil, nil
	}
	if len(msgs) > MaxBatchSize {
		return nil, fmt.Errorf("expopush.Send: batch of %d exceeds max %d", len(msgs), MaxBatchSize)
	}

	body, err := json.Marshal(msgs)
	if err != nil {
		return nil, fmt.Errorf("expopush.Send: marshal: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("expopush.Send: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("expopush.Send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("expopush.Send: unexpected status %d", resp.StatusCode)
	}

	var parsed sendResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("expopush.Send: decode: %w", err)
	}
	if len(parsed.Errors) > 0 {
		return nil, fmt.Errorf("expopush.Send: request error: %s", parsed.Errors[0].Message)
	}
	// A short ticket array would silently mis-attribute results to the wrong
	// tokens, which could prune a live device — treat it as a failed batch.
	if len(parsed.Data) != len(msgs) {
		return nil, fmt.Errorf("expopush.Send: got %d tickets for %d messages", len(parsed.Data), len(msgs))
	}

	out := make([]Result, len(msgs))
	for i, t := range parsed.Data {
		out[i] = Result{Token: msgs[i].To}
		if t.Status == "error" {
			out[i].Unregistered = t.Details.Error == "DeviceNotRegistered"
			out[i].Err = fmt.Errorf("expo push: %s", t.Message)
		}
	}
	return out, nil
}
