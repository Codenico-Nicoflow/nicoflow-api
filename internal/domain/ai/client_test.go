package ai

import (
	"context"
	"testing"
)

// fakeClient proves ai.Client is mockable without any real provider call —
// the whole point of the consumer-owned interface.
type fakeClient struct {
	enabled bool
	stream  Stream
}

func (f fakeClient) Enabled() bool { return f.enabled }

func (f fakeClient) Stream(context.Context, ChatRequest) (Stream, error) { return f.stream, nil }

// fakeStream is a canned two-delta stream with final usage.
type fakeStream struct {
	chunks []string
	i      int
}

func (s *fakeStream) Next() bool {
	if s.i >= len(s.chunks) {
		return false
	}
	s.i++
	return true
}

func (s *fakeStream) Text() string             { return s.chunks[s.i-1] }
func (s *fakeStream) Err() error               { return nil }
func (s *fakeStream) Usage() Usage             { return Usage{InputTokens: 3, OutputTokens: 5} }
func (s *fakeStream) Close() error             { return nil }
func (s *fakeStream) ToolUses() []ToolUseBlock { return nil }
func (s *fakeStream) StopReason() string       { return "end_turn" }

func TestClient_MockableStream(t *testing.T) {
	fc := fakeClient{enabled: true, stream: &fakeStream{chunks: []string{"Hello", " world"}}}

	// A disabled fake reports disabled — this is what the kill switch reads.
	if (fakeClient{enabled: false}).Enabled() {
		t.Fatal("disabled client must report Enabled()==false")
	}

	s, err := fc.Stream(context.Background(), ChatRequest{Model: "m", Messages: []Message{{Role: RoleUser, Text: "hi"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	defer func() { _ = s.Close() }()

	var got string
	for s.Next() {
		got += s.Text()
	}
	if err := s.Err(); err != nil {
		t.Fatalf("stream err: %v", err)
	}
	if got != "Hello world" {
		t.Errorf("accumulated = %q, want %q", got, "Hello world")
	}
	if u := s.Usage(); u.InputTokens != 3 || u.OutputTokens != 5 {
		t.Errorf("usage = %+v, want {3 5}", u)
	}
}

// service must accept the mock client (nil-safe, no real calls at construction).
func TestNewService_AcceptsMockClient(t *testing.T) {
	if NewService(nil, fakeClient{enabled: true}, "claude-haiku-4-5", nil) == nil {
		t.Fatal("NewService returned nil")
	}
}
