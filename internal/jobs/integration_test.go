//go:build integration

package jobs_test

import (
	"context"
	"encoding/json"
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

// stubGC is a no-op AttachmentGC — the reconcile logic is covered by the
// attachment package's own tests; here we only assert the endpoint is wired and
// token-guarded like the digest sweeps.
type stubGC struct{}

func (stubGC) RunGC(context.Context) (jobs.GCSummary, error) {
	return jobs.GCSummary{ObjectsDeleted: 0, RowsDeleted: 0}, nil
}

// stubFocusSweeper stands in for the focus service at the jobs boundary — this
// package tests the route/auth/wiring, not the sweep logic (that lives in the
// focus package's own tests). It echoes dryRun so the propagation is observable.
type stubFocusSweeper struct{}

func (stubFocusSweeper) SweepStale(_ context.Context, dryRun bool) (jobs.FocusSweepResult, error) {
	return jobs.FocusSweepResult{Considered: 2, Closed: 2, DryRun: dryRun}, nil
}

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

// seedProject creates an area + project for userID and returns the project ID.
// tasks.project_id is NOT NULL, so every seeded task needs a real project.
func seedProject(t *testing.T, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	ctx := context.Background()
	areaID := uuid.New().String()
	if _, err := pool.Exec(ctx,
		`INSERT INTO areas (id, user_id, name) VALUES ($1, $2, $3)`,
		areaID, userID, "area "+areaID[:8]); err != nil {
		t.Fatalf("seedProject area: %v", err)
	}
	projectID := uuid.New().String()
	if _, err := pool.Exec(ctx,
		`INSERT INTO projects (id, user_id, area_id, name) VALUES ($1, $2, $3, $4)`,
		projectID, userID, areaID, "project "+projectID[:8]); err != nil {
		t.Fatalf("seedProject project: %v", err)
	}
	return projectID
}

// seedUserWithScheduledTask creates a user in the given timezone with one task
// scheduled for isoDate, and returns the user ID.
func seedUserWithScheduledTask(t *testing.T, pool *pgxpool.Pool, tz, isoDate string) string {
	t.Helper()
	uid := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, username, password_hash, plan, timezone)
		 VALUES ($1, $2, $3, 'x', 'free', $4)`,
		uid, uid+"@jobs.integration.test", "u_"+uid[:8], tz)
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	projectID := seedProject(t, pool, uid)
	_, err = pool.Exec(context.Background(),
		`INSERT INTO tasks (id, user_id, project_id, title, status, scheduled_for)
		 VALUES ($1, $2, $3, 'Scheduled task', 'active', $4)`,
		uuid.New().String(), uid, projectID, isoDate)
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
		jobs.NewDayStartNotifier(repo, notifSvc),
		jobs.NewSummaryNotifier(repo, notifSvc),
		stubGC{},
	).WithFocusStale(stubFocusSweeper{})

	r := chi.NewRouter()
	r.Use(mw.RequestID)
	r.With(mw.InternalToken(cronSecret)).Post("/internal/jobs/day-start", h.DayStart)
	r.With(mw.InternalToken(cronSecret)).Post("/internal/jobs/summary", h.Summary)
	r.With(mw.InternalToken(cronSecret)).Post("/internal/jobs/attachment-gc", h.AttachmentGC)
	r.With(mw.InternalToken(cronSecret)).Post("/internal/jobs/focus-stale", h.FocusStale)
	r.With(mw.InternalToken(cronSecret)).Post("/internal/jobs/run-all", h.RunAll)

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
	if resp := post(t, srv.URL+"/internal/jobs/day-start", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing token → %d, want 401", resp.StatusCode)
	}
	// Wrong token → 401.
	if resp := post(t, srv.URL+"/internal/jobs/day-start", "nope"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token → %d, want 401", resp.StatusCode)
	}
	// Correct token → 200.
	if resp := post(t, srv.URL+"/internal/jobs/day-start", cronSecret); resp.StatusCode != http.StatusOK {
		t.Fatalf("valid token → %d, want 200", resp.StatusCode)
	}

	// attachment-gc is guarded the same way: missing token → 401, valid → 200.
	if resp := post(t, srv.URL+"/internal/jobs/attachment-gc", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("attachment-gc missing token → %d, want 401", resp.StatusCode)
	}
	if resp := post(t, srv.URL+"/internal/jobs/attachment-gc", cronSecret); resp.StatusCode != http.StatusOK {
		t.Fatalf("attachment-gc valid token → %d, want 200", resp.StatusCode)
	}
}

// secretless server → 503.
func TestEndpoint_DisabledWhenSecretUnset(t *testing.T) {
	pool := testutil.NewTestDB(t)
	notifSvc := notification.NewService(notification.NewRepository(pool), nil)
	repo := jobs.NewRepository(pool)
	h := jobs.NewHandler(
		jobs.NewDayStartNotifier(repo, notifSvc),
		jobs.NewSummaryNotifier(repo, notifSvc),
		stubGC{},
	)
	r := chi.NewRouter()
	r.With(mw.InternalToken("")).Post("/internal/jobs/day-start", h.DayStart)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	if resp := post(t, srv.URL+"/internal/jobs/day-start", "anything"); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unset secret → %d, want 503", resp.StatusCode)
	}
}

// TestDayStartSweep_GeneratesAndIsIdempotent drives the morning-digest sweep
// against a real DB with a clock pinned to 08:00 UTC (so it always fires, no
// wall-clock flakiness). A UTC user with a task scheduled today gets exactly one
// morning_digest, and a re-run the same hour produces no duplicate (dedupe_key +
// ON CONFLICT DO NOTHING).
func TestDayStartSweep_GeneratesAndIsIdempotent(t *testing.T) {
	pool := testutil.NewTestDB(t)
	clean(t, pool)
	t.Cleanup(func() { clean(t, pool) })

	pinned := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) // local 08:00 for UTC users
	today := pinned.Format("2006-01-02")
	uid := seedUserWithScheduledTask(t, pool, "UTC", today)

	notifSvc := notification.NewService(notification.NewRepository(pool), nil)
	notifier := jobs.NewDayStartNotifier(jobs.NewRepository(pool), notifSvc)
	jobs.SetDayStartClock(notifier, func() time.Time { return pinned })

	// Run 1 → one notification created.
	n, err := notifier.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if n.Fired != 1 {
		t.Fatalf("run 1 generated = %d, want 1", n.Fired)
	}
	if got := countNotifications(t, pool, uid); got != 1 {
		t.Fatalf("after run 1: %d notifications, want 1", got)
	}

	// Run 2 same hour → no new rows.
	n, err = notifier.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if n.Fired != 0 {
		t.Fatalf("run 2 generated = %d, want 0 (idempotent)", n.Fired)
	}
	if got := countNotifications(t, pool, uid); got != 1 {
		t.Fatalf("after run 2: %d notifications, want 1 (idempotent)", got)
	}
}

// TestDayStartSweep_SkipsTerminalTasks confirms a done task doesn't count toward
// the scheduled figure, and a user with genuinely nothing due stays silent.
func TestDayStartSweep_SkipsTerminalTasks(t *testing.T) {
	pool := testutil.NewTestDB(t)
	clean(t, pool)
	t.Cleanup(func() { clean(t, pool) })

	pinned := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	today := pinned.Format("2006-01-02")

	uid := uuid.New().String()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, username, password_hash, plan, timezone)
		 VALUES ($1, $2, $3, 'x', 'free', 'UTC')`,
		uid, uid+"@jobs.integration.test", "u_"+uid[:8]); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// A done task scheduled today — must be skipped.
	projectID := seedProject(t, pool, uid)
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO tasks (id, user_id, project_id, title, status, scheduled_for)
		 VALUES ($1, $2, $3, 'Done task', 'done', $4)`,
		uuid.New().String(), uid, projectID, today); err != nil {
		t.Fatalf("seed done task: %v", err)
	}

	notifSvc := notification.NewService(notification.NewRepository(pool), nil)
	notifier := jobs.NewDayStartNotifier(jobs.NewRepository(pool), notifSvc)
	jobs.SetDayStartClock(notifier, func() time.Time { return pinned })

	n, err := notifier.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if n.Fired != 0 || countNotifications(t, pool, uid) != 0 {
		t.Fatalf("done task counted toward digest; generated=%d", n.Fired)
	}
}

// TestDayStartSweep_OverdueCounts confirms a task scheduled in the past folds
// into the same digest as an overdue count, without a separate notification.
func TestDayStartSweep_OverdueCounts(t *testing.T) {
	pool := testutil.NewTestDB(t)
	clean(t, pool)
	t.Cleanup(func() { clean(t, pool) })

	pinned := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) // local 08:00 for UTC users
	past := pinned.AddDate(0, 0, -3).Format("2006-01-02")  // scheduled 3 days ago → overdue
	uid := seedUserWithScheduledTask(t, pool, "UTC", past)

	notifSvc := notification.NewService(notification.NewRepository(pool), nil)
	notifier := jobs.NewDayStartNotifier(jobs.NewRepository(pool), notifSvc)
	jobs.SetDayStartClock(notifier, func() time.Time { return pinned })

	n, err := notifier.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("run 1: %v", err)
	}
	if n.Fired != 1 || countNotifications(t, pool, uid) != 1 {
		t.Fatalf("run 1: generated=%d, want 1", n.Fired)
	}

	n, err = notifier.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("run 2: %v", err)
	}
	if n.Fired != 0 || countNotifications(t, pool, uid) != 1 {
		t.Fatalf("run 2: generated=%d / count=%d, want 0 new (idempotent)", n.Fired, countNotifications(t, pool, uid))
	}
}

// AC4 — the focus-stale endpoint is gated by the same shared secret as every
// other internal job: no token or a wrong one is a 401, never a silent run.
func TestFocusStale_Auth(t *testing.T) {
	pool := testutil.NewTestDB(t)
	clean(t, pool)
	t.Cleanup(func() { clean(t, pool) })
	srv := newServer(t, pool)
	url := srv.URL + "/internal/jobs/focus-stale"

	cases := []struct {
		name  string
		token string
		want  int
	}{
		{"missing token", "", http.StatusUnauthorized},
		{"wrong token", "nope", http.StatusUnauthorized},
		{"valid token", cronSecret, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if resp := post(t, url, tc.token); resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

// AC4 (second half) — with CRON_SECRET unset the endpoint is disabled (503),
// never open.
func TestFocusStale_DisabledWhenSecretUnset(t *testing.T) {
	h := (&jobs.Handler{}).WithFocusStale(stubFocusSweeper{})
	r := chi.NewRouter()
	r.With(mw.InternalToken("")).Post("/internal/jobs/focus-stale", h.FocusStale)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	if resp := post(t, srv.URL+"/internal/jobs/focus-stale", "anything"); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unset secret → %d, want 503", resp.StatusCode)
	}
}

// The sweep reports its breakdown, and dryRun is propagated from the query string.
func TestFocusStale_BreakdownAndDryRun(t *testing.T) {
	pool := testutil.NewTestDB(t)
	clean(t, pool)
	t.Cleanup(func() { clean(t, pool) })
	srv := newServer(t, pool)

	for _, dryRun := range []bool{false, true} {
		url := srv.URL + "/internal/jobs/focus-stale"
		if dryRun {
			url += "?dryRun=true"
		}
		resp := post(t, url, cronSecret)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		var env struct {
			Data jobs.FocusSweepResult `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatalf("decode: %v", err)
		}
		resp.Body.Close()
		if env.Data.Considered != 2 || env.Data.DryRun != dryRun {
			t.Fatalf("dryRun=%v → %+v", dryRun, env.Data)
		}
	}
}

// An unwired sweeper (focus disabled) must no-op, not panic or 500.
func TestFocusStale_NilSweeperNoOps(t *testing.T) {
	h := &jobs.Handler{} // focusStale left nil
	r := chi.NewRouter()
	r.With(mw.InternalToken(cronSecret)).Post("/internal/jobs/focus-stale", h.FocusStale)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp := post(t, srv.URL+"/internal/jobs/focus-stale", cronSecret)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("nil sweeper → %d, want 200", resp.StatusCode)
	}
	var env struct {
		Data jobs.FocusSweepResult `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if env.Data.Considered != 0 || env.Data.Closed != 0 {
		t.Fatalf("nil sweeper should report zeroes, got %+v", env.Data)
	}
}

// run-all carries the focus sweep, so the single cron picks it up.
func TestRunAll_IncludesFocusStale(t *testing.T) {
	pool := testutil.NewTestDB(t)
	clean(t, pool)
	t.Cleanup(func() { clean(t, pool) })
	srv := newServer(t, pool)

	resp := post(t, srv.URL+"/internal/jobs/run-all", cronSecret)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var env struct {
		Data jobs.RunAllResult `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if env.Data.FocusStale == nil {
		t.Fatal("run-all payload is missing focusStale")
	}
	if env.Data.FocusStale.Closed != 2 {
		t.Fatalf("focusStale = %+v, want closed=2", env.Data.FocusStale)
	}
}
