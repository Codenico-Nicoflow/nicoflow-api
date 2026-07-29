package focus

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// ── Test doubles ─────────────────────────────────────────────────────────────

type mockRepo struct {
	openAtomic      func(ctx context.Context, s Session) (Session, *Session, error)
	getOpenByUser   func(ctx context.Context, userID string) (Session, bool, error)
	closeOpenByUser func(ctx context.Context, userID string) (Session, bool, error)
	touchLastSeen   func(ctx context.Context, userID, id string) (Session, bool, error)
	touchCurrent    func(ctx context.Context, userID string) (Session, bool, error)
	listStaleOpen   func(ctx context.Context, cutoff time.Time, limit int) ([]Session, error)
	closeByID       func(ctx context.Context, id string) (Session, bool, error)
}

func (m *mockRepo) OpenAtomic(ctx context.Context, s Session) (Session, *Session, error) {
	return m.openAtomic(ctx, s)
}
func (m *mockRepo) GetOpenByUser(ctx context.Context, userID string) (Session, bool, error) {
	return m.getOpenByUser(ctx, userID)
}
func (m *mockRepo) CloseOpenByUser(ctx context.Context, userID string) (Session, bool, error) {
	return m.closeOpenByUser(ctx, userID)
}
func (m *mockRepo) TouchLastSeen(ctx context.Context, userID, id string) (Session, bool, error) {
	return m.touchLastSeen(ctx, userID, id)
}
func (m *mockRepo) TouchCurrent(ctx context.Context, userID string) (Session, bool, error) {
	return m.touchCurrent(ctx, userID)
}

func (m *mockRepo) ListStaleOpen(ctx context.Context, cutoff time.Time, limit int) ([]Session, error) {
	if m.listStaleOpen != nil {
		return m.listStaleOpen(ctx, cutoff, limit)
	}
	return nil, nil
}
func (m *mockRepo) CloseByID(ctx context.Context, id string) (Session, bool, error) {
	if m.closeByID != nil {
		return m.closeByID(ctx, id)
	}
	return Session{}, false, nil
}
func (m *mockRepo) SumClosedSecondsByTask(context.Context, string, string) (int64, error) {
	return 0, nil
}
func (m *mockRepo) SumClosedSecondsByTaskBatch(context.Context, string, []string) (map[string]int64, error) {
	return nil, nil
}

type mockTasks struct {
	openable bool
	err      error
	calls    int
}

func (m *mockTasks) IsOpenable(context.Context, string, string) (bool, error) {
	m.calls++
	return m.openable, m.err
}

// recorder captures emitted events in order — the emit sequence is contractual.
type recorder struct{ events []Event }

func (r *recorder) Broadcast(_ string, ev Event) { r.events = append(r.events, ev) }

func (r *recorder) types() []string {
	out := make([]string, len(r.events))
	for i, ev := range r.events {
		out[i] = ev.Type
	}
	return out
}

const (
	testUserID = "user-1"
	testTaskID = "task-1"
)

func openSession(id, taskID string) Session {
	start := time.Date(2026, 7, 29, 10, 0, 0, 0, time.UTC)
	return Session{ID: id, UserID: testUserID, TaskID: taskID, StartedAt: start, LastSeen: start.Add(time.Minute)}
}

func closedSession(id, taskID string) Session {
	s := openSession(id, taskID)
	ended := s.LastSeen
	s.EndedAt = &ended
	return s
}

func appErrCode(t *testing.T, err error) string {
	t.Helper()
	var ae *apperror.AppError
	if !errors.As(err, &ae) {
		t.Fatalf("expected *apperror.AppError, got %v", err)
	}
	return ae.Code
}

// ── Open ─────────────────────────────────────────────────────────────────────

// AC1 — opening over a prior segment emits ended (old) before started (new), so
// a listening tab stops the old timer before starting the new one.
func TestService_Open_EmitOrder(t *testing.T) {
	cases := []struct {
		name      string
		prior     *Session
		wantTypes []string
	}{
		{
			name:      "no prior segment emits only started",
			wantTypes: []string{EventSessionStarted},
		},
		{
			name:      "prior segment emits ended before started",
			prior:     func() *Session { s := closedSession("prior", "task-old"); return &s }(),
			wantTypes: []string{EventSessionEnded, EventSessionStarted},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opened := openSession("new", testTaskID)
			repo := &mockRepo{
				openAtomic: func(_ context.Context, s Session) (Session, *Session, error) {
					if s.ID == "" {
						t.Fatal("service must generate the session id")
					}
					if s.UserID != testUserID || s.TaskID != testTaskID {
						t.Fatalf("unexpected session passed to repo: %+v", s)
					}
					return opened, tc.prior, nil
				},
			}
			rec := &recorder{}
			svc := NewService(repo, &mockTasks{openable: true}, rec)

			view, err := svc.Open(context.Background(), testUserID, OpenSessionRequest{TaskID: testTaskID})
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if view.ID != opened.ID || view.EndedAt != nil {
				t.Fatalf("unexpected view: %+v", view)
			}

			got := rec.types()
			if len(got) != len(tc.wantTypes) {
				t.Fatalf("emitted %v, want %v", got, tc.wantTypes)
			}
			for i := range got {
				if got[i] != tc.wantTypes[i] {
					t.Fatalf("emitted %v, want %v", got, tc.wantTypes)
				}
			}
			// The ended payload must be the closed prior segment, not the new one.
			if tc.prior != nil {
				ended, ok := rec.events[0].Payload.(SessionView)
				if !ok || ended.ID != tc.prior.ID || ended.EndedAt == nil {
					t.Fatalf("ended payload wrong: %+v", rec.events[0].Payload)
				}
			}
		})
	}
}

// AC2 + input validation: nothing is opened unless the task is owned, non-terminal
// and named.
func TestService_Open_Rejects(t *testing.T) {
	cases := []struct {
		name       string
		taskID     string
		openable   bool
		checkerErr error
		wantCode   string
		wantCheck  bool // whether IsOpenable should have been consulted
	}{
		{name: "empty taskId", taskID: "", wantCode: apperror.ErrInvalidInput},
		{name: "whitespace taskId", taskID: "   ", wantCode: apperror.ErrInvalidInput},
		{name: "unowned or terminal task", taskID: testTaskID, openable: false, wantCode: apperror.ErrTaskNotFound, wantCheck: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			repo := &mockRepo{
				openAtomic: func(context.Context, Session) (Session, *Session, error) {
					t.Fatal("repo must not be reached")
					return Session{}, nil, nil
				},
			}
			tasks := &mockTasks{openable: tc.openable, err: tc.checkerErr}
			rec := &recorder{}
			svc := NewService(repo, tasks, rec)

			_, err := svc.Open(context.Background(), testUserID, OpenSessionRequest{TaskID: tc.taskID})
			if got := appErrCode(t, err); got != tc.wantCode {
				t.Fatalf("code = %s, want %s", got, tc.wantCode)
			}
			if (tasks.calls > 0) != tc.wantCheck {
				t.Fatalf("IsOpenable calls = %d, wantCalled = %v", tasks.calls, tc.wantCheck)
			}
			if len(rec.events) != 0 {
				t.Fatalf("nothing should be emitted, got %v", rec.types())
			}
		})
	}
}

// A repo failure must not emit — the client would otherwise start a timer for a
// segment that was never written.
func TestService_Open_RepoErrorEmitsNothing(t *testing.T) {
	repo := &mockRepo{
		openAtomic: func(context.Context, Session) (Session, *Session, error) {
			return Session{}, nil, errors.New("boom")
		},
	}
	rec := &recorder{}
	svc := NewService(repo, &mockTasks{openable: true}, rec)

	if _, err := svc.Open(context.Background(), testUserID, OpenSessionRequest{TaskID: testTaskID}); err == nil {
		t.Fatal("expected error")
	}
	if len(rec.events) != 0 {
		t.Fatalf("nothing should be emitted, got %v", rec.types())
	}
}

// A nil broadcaster is a valid no-op seam, not a panic.
func TestService_Open_NilBroadcaster(t *testing.T) {
	repo := &mockRepo{
		openAtomic: func(context.Context, Session) (Session, *Session, error) {
			prior := closedSession("prior", "task-old")
			return openSession("new", testTaskID), &prior, nil
		},
	}
	svc := NewService(repo, &mockTasks{openable: true}, nil)
	if _, err := svc.Open(context.Background(), testUserID, OpenSessionRequest{TaskID: testTaskID}); err != nil {
		t.Fatalf("Open: %v", err)
	}
}

// ── CloseCurrent ─────────────────────────────────────────────────────────────

func TestService_CloseCurrent(t *testing.T) {
	t.Run("closes and emits ended", func(t *testing.T) {
		closed := closedSession("open-1", testTaskID)
		repo := &mockRepo{
			closeOpenByUser: func(context.Context, string) (Session, bool, error) {
				return closed, true, nil
			},
		}
		rec := &recorder{}
		svc := NewService(repo, &mockTasks{}, rec)

		view, err := svc.CloseCurrent(context.Background(), testUserID)
		if err != nil {
			t.Fatalf("CloseCurrent: %v", err)
		}
		if view.EndedAt == nil || view.ID != closed.ID {
			t.Fatalf("unexpected view: %+v", view)
		}
		if got := rec.types(); len(got) != 1 || got[0] != EventSessionEnded {
			t.Fatalf("emitted %v, want one session_ended", got)
		}
	})

	// AC3 — closing with nothing open is a 404, not a silent success: pretending
	// it worked would hide a desynced timer.
	t.Run("none open is RESOURCE_NOT_FOUND and emits nothing", func(t *testing.T) {
		repo := &mockRepo{
			closeOpenByUser: func(context.Context, string) (Session, bool, error) {
				return Session{}, false, nil
			},
		}
		rec := &recorder{}
		svc := NewService(repo, &mockTasks{}, rec)

		_, err := svc.CloseCurrent(context.Background(), testUserID)
		if got := appErrCode(t, err); got != apperror.ErrResourceNotFound {
			t.Fatalf("code = %s, want %s", got, apperror.ErrResourceNotFound)
		}
		if len(rec.events) != 0 {
			t.Fatalf("nothing should be emitted, got %v", rec.types())
		}
	})
}

// ── Heartbeat ────────────────────────────────────────────────────────────────

// AC4 — a heartbeat bumps last_seen and stays silent. One statement, so there is
// no read-then-write window a sweep could slip through.
func TestService_Heartbeat(t *testing.T) {
	open := openSession("open-1", testTaskID)

	cases := []struct {
		name     string
		touchOK  bool
		wantCode string // "" = success
	}{
		{name: "bumps an open segment", touchOK: true},
		{name: "none open", touchOK: false, wantCode: apperror.ErrResourceNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var touched bool
			repo := &mockRepo{
				touchCurrent: func(_ context.Context, userID string) (Session, bool, error) {
					touched = true
					if userID != testUserID {
						t.Fatalf("touched %s, want %s", userID, testUserID)
					}
					return open, tc.touchOK, nil
				},
			}
			rec := &recorder{}
			svc := NewService(repo, &mockTasks{}, rec)

			err := svc.Heartbeat(context.Background(), testUserID)
			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("Heartbeat: %v", err)
				}
				if !touched {
					t.Fatal("last_seen was not bumped")
				}
			} else if got := appErrCode(t, err); got != tc.wantCode {
				t.Fatalf("code = %s, want %s", got, tc.wantCode)
			}
			// Silent on every path — a 30s-per-user broadcast would be pure noise.
			if len(rec.events) != 0 {
				t.Fatalf("heartbeat must not broadcast, got %v", rec.types())
			}
		})
	}
}

// ── SweepStale ───────────────────────────────────────────────────────────────

// fixedNow pins the clock so the cutoff is deterministic.
var sweepNow = time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)

// staleSession is an open segment last seen `ago` before the pinned now.
func staleSession(id, userID string, ago time.Duration) Session {
	seen := sweepNow.Add(-ago)
	return Session{ID: id, UserID: userID, TaskID: testTaskID, StartedAt: seen.Add(-time.Hour), LastSeen: seen}
}

// AC1/AC2 — the cutoff is now-StaleThreshold, and every closed segment
// broadcasts session_ended carrying its own closed view.
func TestService_SweepStale_ClosesAndEmits(t *testing.T) {
	stale := []Session{
		staleSession("a", "user-a", 10*time.Minute),
		staleSession("b", "user-b", 5*time.Minute),
	}
	var gotCutoff time.Time
	var gotLimit int
	var closedIDs []string

	repo := &mockRepo{
		listStaleOpen: func(_ context.Context, cutoff time.Time, limit int) ([]Session, error) {
			gotCutoff, gotLimit = cutoff, limit
			return stale, nil
		},
		closeByID: func(_ context.Context, id string) (Session, bool, error) {
			closedIDs = append(closedIDs, id)
			for _, s := range stale {
				if s.ID == id {
					ended := s.LastSeen
					s.EndedAt = &ended
					return s, true, nil
				}
			}
			return Session{}, false, nil
		},
	}
	rec := &recorder{}
	svc := NewServiceWithClock(repo, &mockTasks{}, rec, func() time.Time { return sweepNow })

	got, err := svc.SweepStale(context.Background(), false)
	if err != nil {
		t.Fatalf("SweepStale: %v", err)
	}
	if got.Considered != 2 || got.Closed != 2 || got.DryRun {
		t.Fatalf("breakdown = %+v, want considered=2 closed=2 dryRun=false", got)
	}
	if want := sweepNow.Add(-StaleThreshold); !gotCutoff.Equal(want) {
		t.Fatalf("cutoff = %v, want %v", gotCutoff, want)
	}
	if gotLimit <= 0 {
		t.Fatalf("sweep must be capped, got limit %d", gotLimit)
	}
	if len(closedIDs) != 2 {
		t.Fatalf("closed %v, want both", closedIDs)
	}

	assertAllEndedAtLastSeen(t, rec, 2)
}

// assertAllEndedAtLastSeen checks every emitted event is a session_ended whose
// payload was closed at its own last_seen — the sweep's load-bearing rule.
func assertAllEndedAtLastSeen(t *testing.T, rec *recorder, want int) {
	t.Helper()
	if len(rec.events) != want {
		t.Fatalf("emitted %v, want %d session_ended", rec.types(), want)
	}
	for i, ev := range rec.events {
		if ev.Type != EventSessionEnded {
			t.Fatalf("event %d = %s, want %s", i, ev.Type, EventSessionEnded)
		}
		view, ok := ev.Payload.(SessionView)
		if !ok || view.EndedAt == nil || *view.EndedAt != view.LastSeen {
			t.Fatalf("payload %d should be closed at last_seen: %+v", i, ev.Payload)
		}
	}
}

// AC2 — a fresh segment never reaches the sweep: the repo filters by the cutoff,
// so an empty list is the normal quiet case and must not error or emit.
func TestService_SweepStale_NothingStale(t *testing.T) {
	repo := &mockRepo{
		listStaleOpen: func(context.Context, time.Time, int) ([]Session, error) { return nil, nil },
		closeByID: func(context.Context, string) (Session, bool, error) {
			t.Fatal("nothing stale — close must not be called")
			return Session{}, false, nil
		},
	}
	rec := &recorder{}
	svc := NewServiceWithClock(repo, &mockTasks{}, rec, func() time.Time { return sweepNow })

	got, err := svc.SweepStale(context.Background(), false)
	if err != nil {
		t.Fatalf("SweepStale: %v", err)
	}
	if got.Considered != 0 || got.Closed != 0 {
		t.Fatalf("breakdown = %+v, want zeroes", got)
	}
	if len(rec.events) != 0 {
		t.Fatalf("nothing should be emitted, got %v", rec.types())
	}
}

// AC3 — dry run reports what it would close, touches no row, emits nothing.
func TestService_SweepStale_DryRun(t *testing.T) {
	repo := &mockRepo{
		listStaleOpen: func(context.Context, time.Time, int) ([]Session, error) {
			return []Session{staleSession("a", "user-a", 10*time.Minute)}, nil
		},
		closeByID: func(context.Context, string) (Session, bool, error) {
			t.Fatal("dryRun must not close anything")
			return Session{}, false, nil
		},
	}
	rec := &recorder{}
	svc := NewServiceWithClock(repo, &mockTasks{}, rec, func() time.Time { return sweepNow })

	got, err := svc.SweepStale(context.Background(), true)
	if err != nil {
		t.Fatalf("SweepStale: %v", err)
	}
	if got.Considered != 1 || got.Closed != 0 || !got.DryRun {
		t.Fatalf("breakdown = %+v, want considered=1 closed=0 dryRun=true", got)
	}
	if len(rec.events) != 0 {
		t.Fatalf("dryRun must not broadcast, got %v", rec.types())
	}
}

// Per-item resilience: a row that fails or was already closed must not strand the
// rest of the batch. Only genuinely-closed rows count and emit.
func TestService_SweepStale_PerItemResilience(t *testing.T) {
	stale := []Session{
		staleSession("fails", "user-a", 10*time.Minute),
		staleSession("already-closed", "user-b", 9*time.Minute),
		staleSession("ok", "user-c", 8*time.Minute),
	}
	repo := &mockRepo{
		listStaleOpen: func(context.Context, time.Time, int) ([]Session, error) { return stale, nil },
		closeByID: func(_ context.Context, id string) (Session, bool, error) {
			switch id {
			case "fails":
				return Session{}, false, errors.New("boom")
			case "already-closed":
				// Its own client closed it between the scan and here.
				return Session{}, false, nil
			default:
				s := stale[2]
				ended := s.LastSeen
				s.EndedAt = &ended
				return s, true, nil
			}
		},
	}
	rec := &recorder{}
	svc := NewServiceWithClock(repo, &mockTasks{}, rec, func() time.Time { return sweepNow })

	got, err := svc.SweepStale(context.Background(), false)
	if err != nil {
		t.Fatalf("one bad row must not fail the sweep: %v", err)
	}
	if got.Considered != 3 || got.Closed != 1 {
		t.Fatalf("breakdown = %+v, want considered=3 closed=1", got)
	}
	// Only the genuinely-closed row emits — the failed and already-closed ones must not.
	assertAllEndedAtLastSeen(t, rec, 1)
}

// A listing failure is fatal — there is nothing to sweep and reporting success
// would hide an outage.
func TestService_SweepStale_ListErrorFails(t *testing.T) {
	repo := &mockRepo{
		listStaleOpen: func(context.Context, time.Time, int) ([]Session, error) {
			return nil, errors.New("db down")
		},
	}
	rec := &recorder{}
	svc := NewServiceWithClock(repo, &mockTasks{}, rec, func() time.Time { return sweepNow })

	if _, err := svc.SweepStale(context.Background(), false); err == nil {
		t.Fatal("expected error")
	}
	if len(rec.events) != 0 {
		t.Fatalf("nothing should be emitted, got %v", rec.types())
	}
}
