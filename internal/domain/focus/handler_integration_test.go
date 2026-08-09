//go:build integration

package focus_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/focus"
	"github.com/nicoflow/nicoflow-api/internal/domain/task"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
)

// envelope mirrors the project-wide response shape. error is a structured object,
// never a bare string — the client keys off error.code.
type envelope struct {
	Data  *focus.SessionView `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// spyBroadcaster records what the real service emits through the handler path.
type spyBroadcaster struct{ events []focus.Event }

func (s *spyBroadcaster) Broadcast(_ string, ev focus.Event) { s.events = append(s.events, ev) }

func (s *spyBroadcaster) types() []string {
	out := make([]string, len(s.events))
	for i, ev := range s.events {
		out[i] = ev.Type
	}
	return out
}

// newHandler wires the real repository, service and handler against the test DB,
// so these tests exercise the full stack rather than a mocked seam.
func newHandler(t *testing.T) (*focus.Handler, *spyBroadcaster, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewTestDB(t)
	cleanTestData(t, pool)
	t.Cleanup(func() { cleanTestData(t, pool) })

	spy := &spyBroadcaster{}
	svc := focus.NewService(focus.NewRepository(pool), task.NewRepository(pool), spy)
	return focus.NewHandler(svc), spy, pool
}

// do issues an authenticated request straight at the handler, mirroring what the
// Auth middleware injects.
func do(t *testing.T, h http.HandlerFunc, userID string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(http.MethodPost, "/", nil)
	} else {
		r = httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(body))
	}
	r = r.WithContext(mw.WithAuth(r.Context(), userID, "free"))
	w := httptest.NewRecorder()
	h(w, r)
	return w
}

func decode(t *testing.T, w *httptest.ResponseRecorder) envelope {
	t.Helper()
	var env envelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (body=%s)", err, w.Body.String())
	}
	return env
}

// seedTaskWithStatus creates a task in a given status, for the terminal-status gate.
func seedTaskWithStatus(t *testing.T, pool *pgxpool.Pool, userID, status string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tasks (id, user_id, title, status, display_order)
		 VALUES ($1, $2, 'focus task', $3, 0)`,
		id, userID, status,
	)
	if err != nil {
		t.Fatalf("seedTaskWithStatus: %v", err)
	}
	return id
}

// lastSeenOf reads the user's open segment heartbeat straight from the row.
func lastSeenOf(t *testing.T, pool *pgxpool.Pool, userID string) time.Time {
	t.Helper()
	var ls time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT last_seen FROM focus_sessions WHERE user_id = $1 AND ended_at IS NULL`, userID,
	).Scan(&ls); err != nil {
		t.Fatalf("lastSeenOf: %v", err)
	}
	return ls
}

// AC1 end to end — open on task B while A is open returns 201 with the new
// segment and broadcasts ended(A) before started(B).
func TestHandler_OpenClosesPriorAndBroadcasts(t *testing.T) {
	h, spy, pool := newHandler(t)
	userID := seedUser(t, pool)
	taskA := seedTask(t, pool, userID)
	taskB := seedTask(t, pool, userID)

	w := do(t, h.Open, userID, `{"taskId":"`+taskA+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("open A: status %d body %s", w.Code, w.Body.String())
	}
	first := decode(t, w)
	if first.Error != nil || first.Data == nil || first.Data.TaskID != taskA || first.Data.EndedAt != nil {
		t.Fatalf("unexpected open-A envelope: %+v", first)
	}

	w = do(t, h.Open, userID, `{"taskId":"`+taskB+`"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("open B: status %d body %s", w.Code, w.Body.String())
	}
	second := decode(t, w)
	if second.Data == nil || second.Data.TaskID != taskB {
		t.Fatalf("unexpected open-B envelope: %+v", second)
	}

	// started(A), then ended(A) before started(B).
	want := []string{focus.EventSessionStarted, focus.EventSessionEnded, focus.EventSessionStarted}
	got := spy.types()
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
	ended, ok := spy.events[1].Payload.(focus.SessionView)
	if !ok || ended.ID != first.Data.ID || ended.EndedAt == nil {
		t.Fatalf("ended payload should be the closed A segment, got %+v", spy.events[1].Payload)
	}

	// Exactly one segment is open afterwards.
	var open int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM focus_sessions WHERE user_id = $1 AND ended_at IS NULL`, userID,
	).Scan(&open); err != nil {
		t.Fatalf("count open: %v", err)
	}
	if open != 1 {
		t.Fatalf("expected 1 open segment, got %d", open)
	}
}

// AC2 — a task the caller does not own, or one in a terminal status, is a 404
// TASK_NOT_FOUND and writes nothing.
func TestHandler_OpenRejectsUnownedAndTerminal(t *testing.T) {
	h, spy, pool := newHandler(t)
	userID := seedUser(t, pool)
	otherID := seedUser(t, pool)

	cases := []struct {
		name     string
		body     string
		wantCode int
		wantErr  string
	}{
		{"another user's task", `{"taskId":"` + seedTask(t, pool, otherID) + `"}`, http.StatusNotFound, apperror.ErrTaskNotFound},
		{"nonexistent task", `{"taskId":"` + uuid.NewString() + `"}`, http.StatusNotFound, apperror.ErrTaskNotFound},
		{"done task", `{"taskId":"` + seedTaskWithStatus(t, pool, userID, "done") + `"}`, http.StatusNotFound, apperror.ErrTaskNotFound},
		{"cancelled task", `{"taskId":"` + seedTaskWithStatus(t, pool, userID, "cancelled") + `"}`, http.StatusNotFound, apperror.ErrTaskNotFound},
		{"empty taskId", `{"taskId":""}`, http.StatusBadRequest, apperror.ErrInvalidInput},
		{"malformed body", `{`, http.StatusBadRequest, apperror.ErrInvalidInput},
		{"unknown field", `{"taskId":"x","durationSeconds":9999}`, http.StatusBadRequest, apperror.ErrInvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := do(t, h.Open, userID, tc.body)
			if w.Code != tc.wantCode {
				t.Fatalf("status = %d, want %d (body %s)", w.Code, tc.wantCode, w.Body.String())
			}
			env := decode(t, w)
			if env.Data != nil {
				t.Fatalf("data must be null on error, got %+v", env.Data)
			}
			if env.Error == nil || env.Error.Code != tc.wantErr {
				t.Fatalf("error = %+v, want code %s", env.Error, tc.wantErr)
			}
		})
	}

	var count int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM focus_sessions WHERE user_id = $1`, userID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("no segment should exist, got %d", count)
	}
	if len(spy.events) != 0 {
		t.Fatalf("nothing should be broadcast, got %v", spy.types())
	}
}

// AC3 — close returns the closed segment; closing with none open is a 404.
func TestHandler_Close(t *testing.T) {
	h, spy, pool := newHandler(t)
	userID := seedUser(t, pool)
	taskID := seedTask(t, pool, userID)

	w := do(t, h.Close, userID, "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("close with none open: status %d", w.Code)
	}
	if env := decode(t, w); env.Error == nil || env.Error.Code != apperror.ErrResourceNotFound {
		t.Fatalf("error = %+v, want RESOURCE_NOT_FOUND", env.Error)
	}

	if w := do(t, h.Open, userID, `{"taskId":"`+taskID+`"}`); w.Code != http.StatusCreated {
		t.Fatalf("open: status %d", w.Code)
	}
	w = do(t, h.Close, userID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("close: status %d body %s", w.Code, w.Body.String())
	}
	env := decode(t, w)
	if env.Error != nil || env.Data == nil || env.Data.EndedAt == nil {
		t.Fatalf("closed view should carry endedAt: %+v", env)
	}
	// endedAt is stamped from lastSeen, never the wall clock.
	if *env.Data.EndedAt != env.Data.LastSeen {
		t.Fatalf("endedAt %s should equal lastSeen %s", *env.Data.EndedAt, env.Data.LastSeen)
	}

	if got := spy.types(); len(got) != 2 || got[1] != focus.EventSessionEnded {
		t.Fatalf("events = %v, want [...started, ended]", got)
	}
}

// AC4 — heartbeat bumps last_seen, returns 204 with no body, and stays silent.
func TestHandler_HeartbeatIsSilent(t *testing.T) {
	h, spy, pool := newHandler(t)
	userID := seedUser(t, pool)
	taskID := seedTask(t, pool, userID)

	// No open segment yet.
	w := do(t, h.Heartbeat, userID, "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("heartbeat with none open: status %d", w.Code)
	}
	if env := decode(t, w); env.Error == nil || env.Error.Code != apperror.ErrResourceNotFound {
		t.Fatalf("error = %+v, want RESOURCE_NOT_FOUND", env.Error)
	}

	if w := do(t, h.Open, userID, `{"taskId":"`+taskID+`"}`); w.Code != http.StatusCreated {
		t.Fatalf("open: status %d", w.Code)
	}
	before := lastSeenOf(t, pool, userID)

	w = do(t, h.Heartbeat, userID, "")
	if w.Code != http.StatusNoContent {
		t.Fatalf("heartbeat: status %d body %s", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Fatalf("204 must have an empty body, got %q", w.Body.String())
	}
	if after := lastSeenOf(t, pool, userID); !after.After(before) {
		t.Fatalf("last_seen did not advance: %v → %v", before, after)
	}

	// Only the open broadcast — the heartbeat added nothing.
	if got := spy.types(); len(got) != 1 || got[0] != focus.EventSessionStarted {
		t.Fatalf("heartbeat must not broadcast, events = %v", got)
	}
}
