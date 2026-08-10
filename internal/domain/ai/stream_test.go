package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// ── test doubles ───────────────────────────────────────────────────────────────

// scriptStream yields deltas then an optional terminal error.
type scriptStream struct {
	deltas []string
	i      int
	err    error
	closed bool
}

func (s *scriptStream) Next() bool {
	if s.i >= len(s.deltas) {
		return false
	}
	s.i++
	return true
}
func (s *scriptStream) Text() string             { return s.deltas[s.i-1] }
func (s *scriptStream) Err() error               { return s.err }
func (s *scriptStream) Usage() Usage             { return Usage{InputTokens: 1, OutputTokens: 2} }
func (s *scriptStream) Close() error             { s.closed = true; return nil }
func (s *scriptStream) ToolUses() []ToolUseBlock { return nil }
func (s *scriptStream) StopReason() string       { return "end_turn" }

// scriptClient hands back a scripted stream, or an at-start error.
type scriptClient struct {
	stream   Stream
	startErr error
}

func (c scriptClient) Enabled() bool { return true }
func (c scriptClient) Stream(context.Context, ChatRequest) (Stream, error) {
	if c.startErr != nil {
		return nil, c.startErr
	}
	return c.stream, nil
}

// captureSink records deltas; failAt returns an error on the Nth delta (1-based).
type captureSink struct {
	got    []string
	failAt int
}

func (s *captureSink) Delta(text string) error {
	s.got = append(s.got, text)
	if s.failAt > 0 && len(s.got) == s.failAt {
		return errors.New("client gone")
	}
	return nil
}

func (s *captureSink) ToolProposal(_, _, _ string, _ json.RawMessage) error {
	// Text-only tests never hit this; if they did, treat as observable.
	return nil
}

// stubRepo satisfies Repository with configurable hooks; unset methods no-op.
type stubRepo struct {
	getSession     func(ctx context.Context, userID, id string) (*Session, error)
	reserve        func() (string, error)
	refunded       bool
	userAppends    int
	assistantSaved string
	mu             sync.Mutex
}

func (r *stubRepo) CreateSession(context.Context, Session) (Session, error) { return Session{}, nil }
func (r *stubRepo) ListSessions(context.Context, string) ([]Session, error) { return nil, nil }
func (r *stubRepo) GetSession(ctx context.Context, userID, id string) (*Session, error) {
	if r.getSession != nil {
		return r.getSession(ctx, userID, id)
	}
	return &Session{ID: id, UserID: userID}, nil
}
func (r *stubRepo) ListMessages(_ context.Context, _ string, _ ListMessagesFilter) ([]SessionMessage, string, error) {
	return nil, "", nil
}
func (r *stubRepo) DeleteSession(context.Context, string, string) error        { return nil }
func (r *stubRepo) UsageSum(context.Context, string) (int, error)              { return 0, nil }
func (r *stubRepo) UsageForMonth(context.Context, string, string) (int, error) { return 0, nil }
func (r *stubRepo) ReserveMonthly(context.Context, string, string, int) (string, error) {
	if r.reserve != nil {
		return r.reserve()
	}
	return "usage-1", nil
}
func (r *stubRepo) ReserveLifetime(context.Context, string, string, int) (string, error) {
	if r.reserve != nil {
		return r.reserve()
	}
	return "usage-1", nil
}
func (r *stubRepo) RefundUsage(context.Context, string) error { r.refunded = true; return nil }
func (r *stubRepo) AppendUserMessage(context.Context, string, string, string, string) error {
	r.mu.Lock()
	r.userAppends++
	r.mu.Unlock()
	return nil
}
func (r *stubRepo) AppendAssistantMessage(_ context.Context, _, _, content string) error {
	r.assistantSaved = content
	return nil
}
func (r *stubRepo) HistoryFor(context.Context, string, int) ([]SessionMessage, error) {
	return nil, nil
}
func (r *stubRepo) PromptContext(context.Context, string) (PromptContext, error) {
	return PromptContext{Language: "en"}, nil
}
func (r *stubRepo) AppendAssistantMessageWithBlocks(_ context.Context, _, _, content string, _ json.RawMessage) error {
	r.assistantSaved = content
	return nil
}
func (r *stubRepo) AppendToolResultsMessage(context.Context, string, string, json.RawMessage) error {
	return nil
}
func (r *stubRepo) HistoryForWithBlocks(context.Context, string, int) ([]SessionMessage, error) {
	return nil, nil
}
func (r *stubRepo) InsertPending(context.Context, ToolCall) error { return nil }
func (r *stubRepo) CountForAssistantMessage(context.Context, string, string) (int, error) {
	return 0, nil
}
func (r *stubRepo) ClaimPending(context.Context, string, string, string, string) (ToolCall, error) {
	return ToolCall{}, apperror.New(http.StatusConflict, apperror.ErrConflict, "not pending")
}
func (r *stubRepo) SaveResult(context.Context, string, json.RawMessage) error { return nil }
func (r *stubRepo) ListPendingForSession(context.Context, string, string) ([]ToolCall, error) {
	return nil, nil
}
func (r *stubRepo) GetByToolUseID(context.Context, string, string, string) (ToolCall, error) {
	return ToolCall{}, apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "not found")
}
func (r *stubRepo) ExpirePendingOlderThan(context.Context, time.Time) (int, error) {
	return 0, nil
}

func newSvc(repo Repository, client Client) *service {
	return NewService(repo, client, "test-model", nil).(*service)
}

// ── validation ─────────────────────────────────────────────────────────────────

func TestSendMessage_ValidatesContent(t *testing.T) {
	svc := newSvc(&stubRepo{}, scriptClient{stream: &scriptStream{}})
	for _, tc := range []struct{ name, content string }{
		{"empty", ""},
		{"whitespace only", "   "},
		{"too long", string(make([]byte, 2001))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.SendMessage(context.Background(), "u1", "free", "s1", SendMessageRequest{Content: tc.content}, &captureSink{})
			ae := requireAppErr2(t, err)
			if ae.Status != http.StatusUnprocessableEntity || ae.Code != apperror.ErrInvalidInput {
				t.Errorf("got %d/%s, want 422/INVALID_INPUT", ae.Status, ae.Code)
			}
		})
	}
}

// ── ownership ──────────────────────────────────────────────────────────────────

func TestSendMessage_ForeignSession404(t *testing.T) {
	repo := &stubRepo{getSession: func(context.Context, string, string) (*Session, error) {
		return nil, apperror.New(http.StatusNotFound, apperror.ErrResourceNotFound, "not found")
	}}
	svc := newSvc(repo, scriptClient{stream: &scriptStream{}})
	_, err := svc.SendMessage(context.Background(), "u1", "free", "s1", SendMessageRequest{Content: "hi"}, &captureSink{})
	if ae := requireAppErr2(t, err); ae.Status != http.StatusNotFound {
		t.Errorf("got %d, want 404", ae.Status)
	}
	if repo.userAppends != 0 {
		t.Error("must not persist a message for a foreign session")
	}
}

// ── happy path ─────────────────────────────────────────────────────────────────

func TestSendMessage_StreamsAndPersists(t *testing.T) {
	repo := &stubRepo{}
	svc := newSvc(repo, scriptClient{stream: &scriptStream{deltas: []string{"Hel", "lo"}}})
	sink := &captureSink{}

	msgID, err := svc.SendMessage(context.Background(), "u1", "free", "s1", SendMessageRequest{Content: "hi"}, sink)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if msgID == "" {
		t.Error("expected a persisted assistant message id")
	}
	if got := sink.got; len(got) != 2 || got[0] != "Hel" || got[1] != "lo" {
		t.Errorf("deltas = %v, want [Hel lo]", got)
	}
	if repo.assistantSaved != "Hello" {
		t.Errorf("persisted assistant = %q, want Hello", repo.assistantSaved)
	}
	if repo.refunded {
		t.Error("a successful stream must not refund quota")
	}
}

// ── single-stream guard ────────────────────────────────────────────────────────

func TestSendMessage_SingleStreamGuard(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	// A stream that blocks inside Next until released, holding the guard.
	blocking := &blockStream{entered: entered, release: release}
	svc := newSvc(&stubRepo{}, scriptClient{stream: blocking})

	go func() {
		_, _ = svc.SendMessage(context.Background(), "u1", "free", "s1", SendMessageRequest{Content: "hi"}, &captureSink{})
	}()
	<-entered // first stream is now in flight and holds s1

	_, err := svc.SendMessage(context.Background(), "u1", "free", "s1", SendMessageRequest{Content: "again"}, &captureSink{})
	ae := requireAppErr2(t, err)
	if ae.Status != http.StatusConflict || ae.Code != apperror.ErrAIStreamActive {
		t.Errorf("got %d/%s, want 409/AI_STREAM_ACTIVE", ae.Status, ae.Code)
	}
	close(release)
}

type blockStream struct {
	entered chan struct{}
	release chan struct{}
	done    bool
}

func (s *blockStream) Next() bool {
	if s.done {
		return false
	}
	close(s.entered)
	<-s.release
	s.done = true
	return false
}
func (s *blockStream) Text() string             { return "" }
func (s *blockStream) Err() error               { return nil }
func (s *blockStream) Usage() Usage             { return Usage{} }
func (s *blockStream) Close() error             { return nil }
func (s *blockStream) ToolUses() []ToolUseBlock { return nil }
func (s *blockStream) StopReason() string       { return "end_turn" }

// ── quota exhausted ────────────────────────────────────────────────────────────

func TestSendMessage_QuotaExhausted429(t *testing.T) {
	repo := &stubRepo{reserve: func() (string, error) {
		return "", apperror.New(http.StatusTooManyRequests, apperror.ErrAILimitReached, "limit")
	}}
	svc := newSvc(repo, scriptClient{stream: &scriptStream{deltas: []string{"x"}}})
	sink := &captureSink{}

	_, err := svc.SendMessage(context.Background(), "u1", "free", "s1", SendMessageRequest{Content: "hi"}, sink)
	if ae := requireAppErr2(t, err); ae.Status != http.StatusTooManyRequests {
		t.Errorf("got %d, want 429", ae.Status)
	}
	if len(sink.got) != 0 {
		t.Error("no delta may reach the client when over quota")
	}
	if repo.userAppends != 0 {
		t.Error("no message persisted when over quota (reserve precedes append)")
	}
}

// ── provider error before any token → refund ───────────────────────────────────

func TestSendMessage_ProviderErrorPreStreamRefunds(t *testing.T) {
	repo := &stubRepo{}
	svc := newSvc(repo, scriptClient{startErr: apperror.New(http.StatusBadGateway, apperror.ErrAIProviderError, "boom")})

	_, err := svc.SendMessage(context.Background(), "u1", "free", "s1", SendMessageRequest{Content: "hi"}, &captureSink{})
	if ae := requireAppErr2(t, err); ae.Code != apperror.ErrAIProviderError {
		t.Errorf("got %s, want AI_PROVIDER_ERROR", ae.Code)
	}
	if !repo.refunded {
		t.Error("a zero-token failure must refund the reservation")
	}
}

// ── abort / partial persisted, no refund ───────────────────────────────────────

func TestSendMessage_ClientAbortPersistsPartialNoRefund(t *testing.T) {
	repo := &stubRepo{}
	svc := newSvc(repo, scriptClient{stream: &scriptStream{deltas: []string{"par", "tial"}}})
	sink := &captureSink{failAt: 1} // fail after the first delta (client gone)

	_, err := svc.SendMessage(context.Background(), "u1", "free", "s1", SendMessageRequest{Content: "hi"}, sink)
	if err != nil {
		t.Fatalf("abort is not an error return: %v", err)
	}
	// Only the first delta ("par") was generated + relayed before the client
	// dropped; the loop breaks, so that is the persisted partial.
	if repo.assistantSaved != "par" {
		t.Errorf("partial persisted = %q, want %q", repo.assistantSaved, "par")
	}
	if repo.refunded {
		t.Error("client abort after a token must NOT refund")
	}
}

// ── mid-stream provider error keeps charge + maps code ─────────────────────────

func TestSendMessage_MidStreamErrorKeepsChargeAndMaps(t *testing.T) {
	repo := &stubRepo{}
	provErr := apperror.New(http.StatusServiceUnavailable, apperror.ErrAIUnavailable, "overloaded")
	svc := newSvc(repo, scriptClient{stream: &scriptStream{deltas: []string{"hi"}, err: provErr}})
	sink := &captureSink{}

	_, err := svc.SendMessage(context.Background(), "u1", "free", "s1", SendMessageRequest{Content: "hi"}, sink)
	if err == nil {
		t.Fatal("expected mid-stream error to propagate")
	}
	if repo.refunded {
		t.Error("mid-stream failure keeps the charge (tokens generated)")
	}
	if code := StreamErrorCode(err); code != apperror.ErrAIUnavailable {
		t.Errorf("StreamErrorCode = %s, want AI_UNAVAILABLE", code)
	}
}

func requireAppErr2(t *testing.T, err error) *apperror.AppError {
	t.Helper()
	var ae *apperror.AppError
	if !errors.As(err, &ae) {
		t.Fatalf("expected AppError, got %v", err)
	}
	return ae
}
