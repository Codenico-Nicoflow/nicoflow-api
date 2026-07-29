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

// The sweep + read paths are not exercised by the service; they exist on the
// interface for the jobs layer.
func (m *mockRepo) ListStaleOpen(context.Context, time.Time, int) ([]Session, error) {
	return nil, nil
}
func (m *mockRepo) CloseByID(context.Context, string) (Session, bool, error) {
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
