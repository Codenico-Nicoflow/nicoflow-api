//go:build integration

package focus_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/focus"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
)

const testEmailSuffix = "@focus.integration.test"

func cleanTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	// focus_sessions and tasks cascade from users, but delete leaf-first anyway so
	// a partial failure can't leave orphans behind for the next run.
	queries := []string{
		`DELETE FROM focus_sessions WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%' || $1)`,
		`DELETE FROM tasks WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%' || $1)`,
		`DELETE FROM users WHERE email LIKE '%' || $1`,
	}
	for _, q := range queries {
		if _, err := pool.Exec(context.Background(), q, testEmailSuffix); err != nil {
			t.Fatalf("cleanTestData: %v", err)
		}
	}
}

func newRepo(t *testing.T) (focus.Repository, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewTestDB(t)
	cleanTestData(t, pool)
	t.Cleanup(func() { cleanTestData(t, pool) })
	return focus.NewRepository(pool), pool
}

func seedUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.NewString()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, username, password_hash, plan)
		 VALUES ($1, $2, $3, 'x', 'free')`,
		id, id+testEmailSuffix, "u_"+id[:8],
	)
	if err != nil {
		t.Fatalf("seedUser: %v", err)
	}
	return id
}

func seedTask(t *testing.T, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	id := uuid.NewString()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tasks (id, user_id, title, status, display_order)
		 VALUES ($1, $2, 'focus task', 'active', 0)`,
		id, userID,
	)
	if err != nil {
		t.Fatalf("seedTask: %v", err)
	}
	return id
}

// insertSegment writes a segment directly so tests can control its timestamps —
// the repository always stamps NOW(), which cannot express "started an hour ago".
func insertSegment(t *testing.T, pool *pgxpool.Pool, userID, taskID string, startedAt, lastSeen time.Time, endedAt *time.Time) string {
	t.Helper()
	id := uuid.NewString()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO focus_sessions (id, user_id, task_id, started_at, ended_at, last_seen)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		id, userID, taskID, startedAt, endedAt, lastSeen,
	)
	if err != nil {
		t.Fatalf("insertSegment: %v", err)
	}
	return id
}

func newSession(userID, taskID string) focus.Session {
	return focus.Session{ID: uuid.NewString(), UserID: userID, TaskID: taskID}
}

func mustGetEndedAt(t *testing.T, pool *pgxpool.Pool, id string) time.Time {
	t.Helper()
	var endedAt *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT ended_at FROM focus_sessions WHERE id = $1`, id,
	).Scan(&endedAt); err != nil {
		t.Fatalf("mustGetEndedAt: %v", err)
	}
	if endedAt == nil {
		t.Fatalf("segment %s is still open", id)
	}
	return *endedAt
}

// Open → get → heartbeat → close, all scoped to the user.
func TestRepo_OpenGetTouchClose(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	taskID := seedTask(t, pool, userID)

	opened, closed, err := r.OpenAtomic(ctx, newSession(userID, taskID))
	if err != nil {
		t.Fatalf("OpenAtomic: %v", err)
	}
	if closed != nil {
		t.Fatalf("nothing was open, got closed=%+v", closed)
	}
	if !opened.IsOpen() || opened.TaskID != taskID || opened.StartedAt.IsZero() {
		t.Fatalf("unexpected opened segment: %+v", opened)
	}

	got, ok, err := r.GetOpenByUser(ctx, userID)
	if err != nil || !ok || got.ID != opened.ID {
		t.Fatalf("GetOpenByUser: %+v ok=%v err=%v", got, ok, err)
	}

	touched, ok, err := r.TouchLastSeen(ctx, userID, opened.ID)
	if err != nil || !ok {
		t.Fatalf("TouchLastSeen: ok=%v err=%v", ok, err)
	}
	if !touched.LastSeen.After(opened.LastSeen) && !touched.LastSeen.Equal(opened.LastSeen) {
		t.Fatalf("last_seen went backwards: %v → %v", opened.LastSeen, touched.LastSeen)
	}

	done, ok, err := r.CloseOpenByUser(ctx, userID)
	if err != nil || !ok {
		t.Fatalf("CloseOpenByUser: ok=%v err=%v", ok, err)
	}
	if done.IsOpen() {
		t.Fatalf("segment still open after close: %+v", done)
	}

	if _, ok, err := r.GetOpenByUser(ctx, userID); err != nil || ok {
		t.Fatalf("expected no open segment, ok=%v err=%v", ok, err)
	}
	// A second close is a no-op, not an error.
	if _, ok, err := r.CloseOpenByUser(ctx, userID); err != nil || ok {
		t.Fatalf("double close: ok=%v err=%v", ok, err)
	}
}

// AC1 — opening on a second task closes the first segment and inserts the new one
// atomically, and the close stamps last_seen.
func TestRepo_OpenAtomic_ClosesPriorSegment(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	taskA := seedTask(t, pool, userID)
	taskB := seedTask(t, pool, userID)

	lastSeen := time.Now().Add(-10 * time.Minute).UTC().Truncate(time.Millisecond)
	priorID := insertSegment(t, pool, userID, taskA, lastSeen.Add(-20*time.Minute), lastSeen, nil)

	opened, closed, err := r.OpenAtomic(ctx, newSession(userID, taskB))
	if err != nil {
		t.Fatalf("OpenAtomic: %v", err)
	}
	if closed == nil || closed.ID != priorID {
		t.Fatalf("expected prior segment closed, got %+v", closed)
	}
	if !closed.EndedAt.Equal(lastSeen) {
		t.Fatalf("ended_at should equal last_seen %v, got %v", lastSeen, *closed.EndedAt)
	}
	if opened.TaskID != taskB || !opened.IsOpen() {
		t.Fatalf("unexpected new segment: %+v", opened)
	}

	// Exactly one open row survives.
	var openCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM focus_sessions WHERE user_id = $1 AND ended_at IS NULL`, userID,
	).Scan(&openCount); err != nil {
		t.Fatalf("count open: %v", err)
	}
	if openCount != 1 {
		t.Fatalf("expected exactly 1 open segment, got %d", openCount)
	}
}

// AC1 under contention — concurrent opens for one user must leave exactly one
// open segment, lose no close, and never surface the unique-index violation to
// the caller. This is the case a row lock alone cannot cover: with no segment yet
// open there is no row to lock, so the opens serialize on the advisory lock.
func TestRepo_OpenAtomic_ConcurrentOpensKeepOneOpen(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)

	const n = 8
	taskIDs := make([]string, n)
	for i := range taskIDs {
		taskIDs[i] = seedTask(t, pool, userID)
	}

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for _, taskID := range taskIDs {
		wg.Add(1)
		go func(taskID string) {
			defer wg.Done()
			if _, _, err := r.OpenAtomic(ctx, newSession(userID, taskID)); err != nil {
				errs <- err
			}
		}(taskID)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent OpenAtomic: %v", err)
	}

	var open, total int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FILTER (WHERE ended_at IS NULL), COUNT(*)
		 FROM focus_sessions WHERE user_id = $1`, userID,
	).Scan(&open, &total); err != nil {
		t.Fatalf("count: %v", err)
	}
	if open != 1 {
		t.Fatalf("expected exactly 1 open segment, got %d", open)
	}
	if total != n {
		t.Fatalf("expected %d segments (one per open), got %d", n, total)
	}

	// Every close along the way stamped last_seen rather than the wall clock.
	var mismatched int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM focus_sessions
		 WHERE user_id = $1 AND ended_at IS NOT NULL AND ended_at <> last_seen`, userID,
	).Scan(&mismatched); err != nil {
		t.Fatalf("count mismatched: %v", err)
	}
	if mismatched != 0 {
		t.Fatalf("%d closed segments did not stamp last_seen", mismatched)
	}
}

// AC2 — the partial-unique index rejects a second open row written outside the
// repository's transaction.
func TestRepo_PartialUniqueIndex_RejectsSecondOpen(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	taskID := seedTask(t, pool, userID)

	if _, _, err := r.OpenAtomic(ctx, newSession(userID, taskID)); err != nil {
		t.Fatalf("OpenAtomic: %v", err)
	}

	_, err := pool.Exec(ctx,
		`INSERT INTO focus_sessions (id, user_id, task_id) VALUES ($1, $2, $3)`,
		uuid.NewString(), userID, taskID,
	)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("expected unique violation 23505, got %v", err)
	}

	// A closed segment is exempt from the index — the user may accumulate many.
	ended := time.Now().UTC()
	insertSegment(t, pool, userID, taskID, ended.Add(-time.Minute), ended, &ended)
}

// A task belonging to another user is indistinguishable from a missing one, and
// neither can accrue time.
func TestRepo_OpenAtomic_RejectsForeignAndMissingTask(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	otherID := seedUser(t, pool)
	foreignTask := seedTask(t, pool, otherID)

	cases := []struct {
		name   string
		taskID string
	}{
		{"another user's task", foreignTask},
		{"nonexistent task", uuid.NewString()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := r.OpenAtomic(ctx, newSession(userID, tc.taskID))
			var appErr *apperror.AppError
			if !errors.As(err, &appErr) || appErr.Code != apperror.ErrTaskNotFound {
				t.Fatalf("expected TASK_NOT_FOUND, got %v", err)
			}
			var count int
			if err := pool.QueryRow(ctx,
				`SELECT COUNT(*) FROM focus_sessions WHERE user_id = $1`, userID,
			).Scan(&count); err != nil {
				t.Fatalf("count: %v", err)
			}
			if count != 0 {
				t.Fatalf("expected no segment written, got %d", count)
			}
		})
	}
}

// A heartbeat can only extend the caller's own open segment.
func TestRepo_TouchLastSeen_ScopedAndClosedAware(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	otherID := seedUser(t, pool)
	taskID := seedTask(t, pool, userID)

	opened, _, err := r.OpenAtomic(ctx, newSession(userID, taskID))
	if err != nil {
		t.Fatalf("OpenAtomic: %v", err)
	}

	cases := []struct {
		name    string
		userID  string
		id      string
		wantOK  bool
		prepare func()
	}{
		{name: "another user's segment", userID: otherID, id: opened.ID},
		{name: "unknown segment", userID: userID, id: uuid.NewString()},
		{name: "own open segment", userID: userID, id: opened.ID, wantOK: true},
		{
			name:    "already closed segment",
			userID:  userID,
			id:      opened.ID,
			prepare: func() { _, _, _ = r.CloseOpenByUser(ctx, userID) },
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.prepare != nil {
				tc.prepare()
			}
			_, ok, err := r.TouchLastSeen(ctx, tc.userID, tc.id)
			if err != nil {
				t.Fatalf("TouchLastSeen: %v", err)
			}
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
		})
	}
}

// AC3 — the sweep closes stale segments at last_seen, not now, and is idempotent.
func TestRepo_ListStaleOpenAndCloseByID(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	otherID := seedUser(t, pool)
	taskID := seedTask(t, pool, userID)
	otherTask := seedTask(t, pool, otherID)

	now := time.Now().UTC().Truncate(time.Millisecond)
	staleSeen := now.Add(-30 * time.Minute)
	staleID := insertSegment(t, pool, userID, taskID, staleSeen.Add(-time.Hour), staleSeen, nil)
	// Another user's stale segment — the sweep is system-scope and must see it.
	otherStaleID := insertSegment(t, pool, otherID, otherTask, staleSeen.Add(-time.Hour), staleSeen.Add(time.Minute), nil)
	// Fresh: last_seen after the cutoff, so it must survive the sweep.
	freshSeen := now.Add(-time.Minute)
	freshUser := seedUser(t, pool)
	freshTask := seedTask(t, pool, freshUser)
	freshID := insertSegment(t, pool, freshUser, freshTask, freshSeen, freshSeen, nil)

	cutoff := now.Add(-5 * time.Minute)
	stale, err := r.ListStaleOpen(ctx, cutoff, 10)
	if err != nil {
		t.Fatalf("ListStaleOpen: %v", err)
	}
	if len(stale) != 2 {
		t.Fatalf("expected 2 stale segments, got %d: %+v", len(stale), stale)
	}
	// Oldest first.
	if stale[0].ID != staleID || stale[1].ID != otherStaleID {
		t.Fatalf("unexpected order: %s, %s", stale[0].ID, stale[1].ID)
	}
	for _, s := range stale {
		if s.ID == freshID {
			t.Fatal("fresh segment must not be swept")
		}
	}

	closed, ok, err := r.CloseByID(ctx, staleID)
	if err != nil || !ok {
		t.Fatalf("CloseByID: ok=%v err=%v", ok, err)
	}
	if !closed.EndedAt.Equal(staleSeen) {
		t.Fatalf("ended_at should equal last_seen %v, got %v", staleSeen, *closed.EndedAt)
	}
	if got := mustGetEndedAt(t, pool, staleID); !got.Equal(staleSeen) {
		t.Fatalf("persisted ended_at = %v, want %v", got, staleSeen)
	}

	// Idempotent: closing again reports ok=false and leaves ended_at untouched.
	if _, ok, err := r.CloseByID(ctx, staleID); err != nil || ok {
		t.Fatalf("second CloseByID: ok=%v err=%v", ok, err)
	}
	if got := mustGetEndedAt(t, pool, staleID); !got.Equal(staleSeen) {
		t.Fatalf("ended_at changed on re-close: %v", got)
	}
	// An unknown id is a no-op too.
	if _, ok, err := r.CloseByID(ctx, uuid.NewString()); err != nil || ok {
		t.Fatalf("CloseByID unknown: ok=%v err=%v", ok, err)
	}

	// The limit bounds one sweep run.
	capped, err := r.ListStaleOpen(ctx, cutoff, 1)
	if err != nil || len(capped) != 1 {
		t.Fatalf("ListStaleOpen limit: got %d err=%v", len(capped), err)
	}
}

// AC4 — the total sums closed segments only, excludes the open one, and is 0 when
// there are none. Cross-user rows never leak into another user's total.
func TestRepo_SumClosedSeconds(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	otherID := seedUser(t, pool)
	taskA := seedTask(t, pool, userID)
	taskB := seedTask(t, pool, userID)
	emptyTask := seedTask(t, pool, userID)

	now := time.Now().UTC()
	// Three closed segments on task A: 60 + 120 + 30 = 210s.
	for _, secs := range []int{60, 120, 30} {
		start := now.Add(-time.Duration(secs+600) * time.Second)
		end := start.Add(time.Duration(secs) * time.Second)
		insertSegment(t, pool, userID, taskA, start, end, &end)
	}
	// One open segment on task A — excluded, its end is not known yet.
	insertSegment(t, pool, userID, taskA, now.Add(-5*time.Minute), now, nil)
	// 45s on task B.
	bStart := now.Add(-time.Hour)
	bEnd := bStart.Add(45 * time.Second)
	insertSegment(t, pool, userID, taskB, bStart, bEnd, &bEnd)
	// Another user's closed segment on a task id that is not the caller's.
	otherTask := seedTask(t, pool, otherID)
	oStart := now.Add(-time.Hour)
	oEnd := oStart.Add(999 * time.Second)
	insertSegment(t, pool, otherID, otherTask, oStart, oEnd, &oEnd)

	cases := []struct {
		name   string
		userID string
		taskID string
		want   int64
	}{
		{"sums closed segments, excludes open", userID, taskA, 210},
		{"single closed segment", userID, taskB, 45},
		{"no segments is zero", userID, emptyTask, 0},
		{"another user's task is zero", userID, otherTask, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := r.SumClosedSecondsByTask(ctx, tc.userID, tc.taskID)
			if err != nil {
				t.Fatalf("SumClosedSecondsByTask: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %d seconds, want %d", got, tc.want)
			}
		})
	}

	t.Run("batch matches the per-task sums", func(t *testing.T) {
		got, err := r.SumClosedSecondsByTaskBatch(ctx, userID, []string{taskA, taskB, emptyTask, otherTask})
		if err != nil {
			t.Fatalf("SumClosedSecondsByTaskBatch: %v", err)
		}
		if got[taskA] != 210 || got[taskB] != 45 {
			t.Fatalf("unexpected batch totals: %+v", got)
		}
		// Tasks with no closed segments are absent, which callers read as 0.
		if _, ok := got[emptyTask]; ok {
			t.Fatalf("empty task should be absent: %+v", got)
		}
		if _, ok := got[otherTask]; ok {
			t.Fatalf("another user's task must not appear: %+v", got)
		}
	})

	t.Run("empty id list is an empty map", func(t *testing.T) {
		got, err := r.SumClosedSecondsByTaskBatch(ctx, userID, nil)
		if err != nil || len(got) != 0 {
			t.Fatalf("got %+v err=%v", got, err)
		}
	})
}

// Deleting a task or a user reclaims its segments through the FK cascades.
func TestRepo_CascadeOnTaskAndUserDelete(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	taskID := seedTask(t, pool, userID)

	if _, _, err := r.OpenAtomic(ctx, newSession(userID, taskID)); err != nil {
		t.Fatalf("OpenAtomic: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM tasks WHERE id = $1`, taskID); err != nil {
		t.Fatalf("delete task: %v", err)
	}
	if _, ok, err := r.GetOpenByUser(ctx, userID); err != nil || ok {
		t.Fatalf("segment survived task delete: ok=%v err=%v", ok, err)
	}

	task2 := seedTask(t, pool, userID)
	if _, _, err := r.OpenAtomic(ctx, newSession(userID, task2)); err != nil {
		t.Fatalf("OpenAtomic: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM focus_sessions WHERE user_id = $1`, userID,
	).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("segments survived user delete: %d", count)
	}
}
