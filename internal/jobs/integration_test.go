//go:build integration

package jobs_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
	"github.com/nicoflow/nicoflow-api/internal/jobs"
	mw "github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
)

const cronSecret = "test-cron-secret"

func clean(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	queries := []string{
		`DELETE FROM notifications WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@jobs.integration.test')`,
		`DELETE FROM notification_preferences WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@jobs.integration.test')`,
		`DELETE FROM tasks WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@jobs.integration.test')`,
		`DELETE FROM users WHERE email LIKE '%@jobs.integration.test'`,
	}
	for _, q := range queries {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			t.Fatalf("clean: %v", err)
		}
	}
}

// seedUserWithDueTask creates a user in the given timezone with one task scheduled
// for isoDate, and returns the user ID.
func seedUserWithDueTask(t *testing.T, pool *pgxpool.Pool, tz, isoDate string) string {
	t.Helper()
	uid := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, username, password_hash, plan, timezone)
		 VALUES ($1, $2, $3, 'x', 'free', $4)`,
		uid, uid+"@jobs.integration.test", "u_"+uid[:8], tz)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO tasks (id, user_id, title, status, scheduled_for)
		 VALUES ($1, $2, 'Due task', 'active', $3)`,
		uuid.New().String(), uid, isoDate)
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}
	return uid
}

func countNotifications(t *testing.T, pool *pgxpool.Pool, userID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM notifications WHERE user_id = $1`, userID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

// newServer wires the jobs route exactly as router.New does — InternalToken guard,
// outside any JWT group — over a real notification service and jobs repo.
func newServer(t *testing.T, pool *pgxpool.Pool) *httptest.Server {
	t.Helper()
	notifSvc := notification.NewService(notification.NewRepository(pool), nil)
	repo := jobs.NewRepository(pool)
	h := jobs.NewHandler(
		jobs.NewDueDateNotifier(repo, notifSvc, ""),
		jobs.NewOverdueNotifier(repo, notifSvc),
		jobs.NewDayStartNotifier(repo, notifSvc),
	)

	r := chi.NewRouter()
	r.Use(mw.RequestID)
	r.With(mw.InternalToken(cronSecret)).Post("/internal/jobs/due-notify", h.DueNotify)
	r.With(mw.InternalToken(cronSecret)).Post("/internal/jobs/overdue", h.OverdueNotify)
	r.With(mw.InternalToken(cronSecret)).Post("/internal/jobs/day-start", h.DayStart)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func post(t *testing.T, url, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("X-Internal-Token", token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	return resp
}

func TestEndpoint_Auth(t *testing.T) {
	pool := testutil.NewTestDB(t)
	clean(t, pool)
	t.Cleanup(func() { clean(t, pool) })
	srv := newServer(t, pool)

	// Missing token → 401.
	if resp := post(t, srv.URL+"/internal/jobs/due-notify", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing token → %d, want 401", resp.StatusCode)
	}
	// Wrong token → 401.
	if resp := post(t, srv.URL+"/internal/jobs/due-notify", "nope"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token → %d, want 401", resp.StatusCode)
	}
	// Correct token → 200.
	if resp := post(t, srv.URL+"/internal/jobs/due-notify", cronSecret); resp.StatusCode != http.StatusOK {
		t.Fatalf("valid token → %d, want 200", resp.StatusCode)
	}
}

// secretless server → 503.
func TestEndpoint_DisabledWhenSecretUnset(t *testing.T) {
	pool := testutil.NewTestDB(t)
	notifSvc := notification.NewService(notification.NewRepository(pool), nil)
	repo := jobs.NewRepository(pool)
	h := jobs.NewHandler(
		jobs.NewDueDateNotifier(repo, notifSvc, ""),
		jobs.NewOverdueNotifier(repo, notifSvc),
		jobs.NewDayStartNotifier(repo, notifSvc),
	)
	r := chi.NewRouter()
	r.With(mw.InternalToken("")).Post("/internal/jobs/due-notify", h.DueNotify)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	if resp := post(t, srv.URL+"/internal/jobs/due-notify", "anything"); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unset secret → %d, want 503", resp.StatusCode)
	}
}

// TestSweep_GeneratesAndIsIdempotent drives the sweep against a real DB with a
// clock pinned to 08:00 UTC (so it always fires, no wall-clock flakiness). A UTC
// user with a task due local-tomorrow gets exactly one notification, and a re-run
// the same hour produces no duplicate (dedupe_key + ON CONFLICT DO NOTHING).
func TestSweep_GeneratesAndIsIdempotent(t *testing.T) {
	pool := testutil.NewTestDB(t)
	clean(t, pool)
	t.Cleanup(func() { clean(t, pool) })

	pinned := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) // local 08:00 for UTC users
	target := pinned.AddDate(0, 0, 1).Format("2006-01-02") // 1440-min lead → tomorrow
	uid := seedUserWithDueTask(t, pool, "UTC", target)

	notifSvc := notification.NewService(notification.NewRepository(pool), nil)
	notifier := jobs.NewDueDateNotifier(jobs.NewRepository(pool), notifSvc, "")
	jobs.SetClock(notifier, func() time.Time { return pinned })

	// Run 1 → one notification created.
	n, err := notifier.Run(context.Background())
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if n != 1 {
		t.Fatalf("run 1 generated = %d, want 1", n)
	}
	if got := countNotifications(t, pool, uid); got != 1 {
		t.Fatalf("after run 1: %d notifications, want 1", got)
	}

	// Run 2 same hour → no new rows.
	n, err = notifier.Run(context.Background())
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if n != 0 {
		t.Fatalf("run 2 generated = %d, want 0 (idempotent)", n)
	}
	if got := countNotifications(t, pool, uid); got != 1 {
		t.Fatalf("after run 2: %d notifications, want 1 (idempotent)", got)
	}
}

// TestSweep_SkipsTerminalAndUnscheduled confirms only non-terminal tasks scheduled
// on the target date generate a reminder.
func TestSweep_SkipsTerminalTasks(t *testing.T) {
	pool := testutil.NewTestDB(t)
	clean(t, pool)
	t.Cleanup(func() { clean(t, pool) })

	pinned := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	target := pinned.AddDate(0, 0, 1).Format("2006-01-02")

	uid := uuid.New().String()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, username, password_hash, plan, timezone)
		 VALUES ($1, $2, $3, 'x', 'free', 'UTC')`,
		uid, uid+"@jobs.integration.test", "u_"+uid[:8]); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// A done task on the target date — must be skipped.
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO tasks (id, user_id, title, status, scheduled_for)
		 VALUES ($1, $2, 'Done task', 'done', $3)`,
		uuid.New().String(), uid, target); err != nil {
		t.Fatalf("seed done task: %v", err)
	}

	notifSvc := notification.NewService(notification.NewRepository(pool), nil)
	notifier := jobs.NewDueDateNotifier(jobs.NewRepository(pool), notifSvc, "")
	jobs.SetClock(notifier, func() time.Time { return pinned })

	n, err := notifier.Run(context.Background())
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if n != 0 || countNotifications(t, pool, uid) != 0 {
		t.Fatalf("terminal task generated a reminder; generated=%d", n)
	}
}

// TestOverdueSweep_GeneratesAndIsIdempotent drives the overdue sweep against a
// real DB with the clock pinned to local 08:00. A UTC user with a task scheduled
// in the past gets exactly one task_overdue, and a re-run the same day adds none.
func TestOverdueSweep_GeneratesAndIsIdempotent(t *testing.T) {
	pool := testutil.NewTestDB(t)
	clean(t, pool)
	t.Cleanup(func() { clean(t, pool) })

	pinned := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) // local 08:00 for UTC users
	past := pinned.AddDate(0, 0, -3).Format("2006-01-02")  // scheduled 3 days ago → overdue
	uid := seedUserWithDueTask(t, pool, "UTC", past)

	notifSvc := notification.NewService(notification.NewRepository(pool), nil)
	notifier := jobs.NewOverdueNotifier(jobs.NewRepository(pool), notifSvc)
	jobs.SetOverdueClock(notifier, func() time.Time { return pinned })

	n, err := notifier.Run(context.Background())
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if n != 1 || countNotifications(t, pool, uid) != 1 {
		t.Fatalf("run 1: generated=%d, want 1", n)
	}

	n, err = notifier.Run(context.Background())
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if n != 0 || countNotifications(t, pool, uid) != 1 {
		t.Fatalf("run 2: generated=%d / count=%d, want 0 new (idempotent)", n, countNotifications(t, pool, uid))
	}
}
