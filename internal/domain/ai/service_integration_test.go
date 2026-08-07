//go:build integration

package ai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/ai"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
)

// ── mocked provider ────────────────────────────────────────────────────────────

type fakeProvider struct {
	deltas    []string
	streamErr error // terminal Err() surfaced after the deltas
	startErr  error // returned by Stream before any token
}

func (p fakeProvider) Enabled() bool { return true }
func (p fakeProvider) Stream(context.Context, ai.ChatRequest) (ai.Stream, error) {
	if p.startErr != nil {
		return nil, p.startErr
	}
	return &fakeProviderStream{deltas: p.deltas, err: p.streamErr}, nil
}

type fakeProviderStream struct {
	deltas []string
	i      int
	err    error
}

func (s *fakeProviderStream) Next() bool {
	if s.i >= len(s.deltas) {
		return false
	}
	s.i++
	return true
}
func (s *fakeProviderStream) Text() string                   { return s.deltas[s.i-1] }
func (s *fakeProviderStream) Err() error                     { return s.err }
func (s *fakeProviderStream) Usage() ai.Usage                { return ai.Usage{InputTokens: 4, OutputTokens: 6} }
func (s *fakeProviderStream) Close() error                   { return nil }
func (s *fakeProviderStream) ToolUses() []ai.ToolUseBlock    { return nil }
func (s *fakeProviderStream) StopReason() string             { return "end_turn" }

// collectSink records every delta; failAfter>0 simulates a client drop.
type collectSink struct {
	got       []string
	failAfter int
}

func (s *collectSink) Delta(text string) error {
	s.got = append(s.got, text)
	if s.failAfter > 0 && len(s.got) >= s.failAfter {
		return errors.New("client disconnected")
	}
	return nil
}

func (s *collectSink) ToolProposal(_, _, _ string, _ json.RawMessage) error { return nil }

func messageRows(t *testing.T, pool *pgxpool.Pool, sessionID string) []struct {
	Role    string
	Content string
} {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT role, content FROM ai_messages WHERE session_id = $1 ORDER BY created_at ASC, id ASC`,
		sessionID)
	if err != nil {
		t.Fatalf("messageRows: %v", err)
	}
	defer rows.Close()
	var out []struct {
		Role    string
		Content string
	}
	for rows.Next() {
		var r struct {
			Role    string
			Content string
		}
		if err := rows.Scan(&r.Role, &r.Content); err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, r)
	}
	return out
}

func sessionTitle(t *testing.T, pool *pgxpool.Pool, id string) string {
	t.Helper()
	var title string
	if err := pool.QueryRow(context.Background(),
		`SELECT title FROM ai_sessions WHERE id = $1`, id).Scan(&title); err != nil {
		t.Fatalf("sessionTitle: %v", err)
	}
	return title
}

// ── full flow ──────────────────────────────────────────────────────────────────

func TestService_SendMessage_FullFlow(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanAITestData(t, pool)
	t.Cleanup(func() { cleanAITestData(t, pool) })

	userID := seedUser(t, pool, "pro")
	svc := ai.NewService(ai.NewRepository(pool), fakeProvider{deltas: []string{"Hello, ", "how can I help?"}}, "test-model", nil)

	sess, err := svc.CreateSession(context.Background(), userID, ai.CreateSessionRequest{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	sink := &collectSink{}
	msgID, err := svc.SendMessage(context.Background(), userID, "pro", sess.ID,
		ai.SendMessageRequest{Content: "This is my first question about productivity"}, sink)
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if msgID == "" {
		t.Fatal("want a persisted assistant message id")
	}

	// Both turns persisted, in order.
	rows := messageRows(t, pool, sess.ID)
	if len(rows) != 2 {
		t.Fatalf("want 2 messages, got %d", len(rows))
	}
	if rows[0].Role != "user" || rows[1].Role != "assistant" {
		t.Errorf("roles = %s,%s want user,assistant", rows[0].Role, rows[1].Role)
	}
	if rows[1].Content != "Hello, how can I help?" {
		t.Errorf("assistant content = %q", rows[1].Content)
	}

	// First message seeded the title (word-boundary ≤50).
	if got := sessionTitle(t, pool, sess.ID); got != "This is my first question about productivity" {
		t.Errorf("title = %q", got)
	}

	// Quota charged exactly once.
	if used := usageCount(t, pool, userID); used != 1 {
		t.Errorf("usage = %d, want 1", used)
	}
}

func TestService_SendMessage_QuotaExhausted(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanAITestData(t, pool)
	t.Cleanup(func() { cleanAITestData(t, pool) })

	userID := seedUser(t, pool, "free")
	// Free lifetime limit is 5 — seed it to the cap for the current month.
	seedUsage(t, pool, userID, monthStart(), ai.FreeLifetimeLimit)

	svc := ai.NewService(ai.NewRepository(pool), fakeProvider{deltas: []string{"x"}}, "test-model", nil)
	sess, err := svc.CreateSession(context.Background(), userID, ai.CreateSessionRequest{})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	sink := &collectSink{}
	_, err = svc.SendMessage(context.Background(), userID, "free", sess.ID,
		ai.SendMessageRequest{Content: "hi"}, sink)
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Status != http.StatusTooManyRequests {
		t.Fatalf("want 429 AppError, got %v", err)
	}
	if len(sink.got) != 0 {
		t.Error("no token may reach the client when over quota")
	}
	if len(messageRows(t, pool, sess.ID)) != 0 {
		t.Error("no message persisted when over quota")
	}
}

func TestService_SendMessage_AbortPersistsPartial(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanAITestData(t, pool)
	t.Cleanup(func() { cleanAITestData(t, pool) })

	userID := seedUser(t, pool, "pro")
	svc := ai.NewService(ai.NewRepository(pool), fakeProvider{deltas: []string{"partial ", "more"}}, "test-model", nil)
	sess, _ := svc.CreateSession(context.Background(), userID, ai.CreateSessionRequest{})

	sink := &collectSink{failAfter: 1} // drop after the first delta
	_, err := svc.SendMessage(context.Background(), userID, "pro", sess.ID,
		ai.SendMessageRequest{Content: "hi"}, sink)
	if err != nil {
		t.Fatalf("abort should not error: %v", err)
	}

	rows := messageRows(t, pool, sess.ID)
	if len(rows) != 2 || rows[1].Content != "partial " {
		t.Errorf("want partial assistant row 'partial ', got %+v", rows)
	}
	// Charge kept — a token was generated.
	if used := usageCount(t, pool, userID); used != 1 {
		t.Errorf("usage = %d, want 1 (abort keeps charge)", used)
	}
}

// monthStart mirrors the service's YYYY-MM-01 month key for the current month.
func monthStart() string {
	return time.Now().UTC().Format("2006-01") + "-01"
}
