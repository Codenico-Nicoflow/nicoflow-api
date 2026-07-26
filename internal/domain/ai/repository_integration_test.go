//go:build integration

package ai_test

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/ai"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
)

const testEmailDomain = "@ai-integration.test"

func cleanAITestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	queries := []string{
		`DELETE FROM ai_messages WHERE session_id IN (
			SELECT s.id FROM ai_sessions s JOIN users u ON u.id = s.user_id
			WHERE u.email LIKE '%' || $1)`,
		`DELETE FROM ai_sessions WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%' || $1)`,
		`DELETE FROM ai_usage_monthly WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%' || $1)`,
		`DELETE FROM users WHERE email LIKE '%' || $1`,
	}
	for _, q := range queries {
		if _, err := pool.Exec(context.Background(), q, testEmailDomain); err != nil {
			t.Fatalf("cleanAITestData: %v", err)
		}
	}
}

func seedUser(t *testing.T, pool *pgxpool.Pool, plan string) string {
	t.Helper()
	id := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, username, password_hash, plan) VALUES ($1, $2, $3, 'x', $4)`,
		id, id+testEmailDomain, uuid.New().String()[:12], plan,
	)
	if err != nil {
		t.Fatalf("seedUser: %v", err)
	}
	return id
}

func seedMessage(t *testing.T, pool *pgxpool.Pool, sessionID, role, content string, createdAt time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO ai_messages (id, session_id, role, content, created_at) VALUES ($1, $2, $3, $4, $5)`,
		uuid.New().String(), sessionID, role, content, createdAt,
	)
	if err != nil {
		t.Fatalf("seedMessage: %v", err)
	}
}

func seedUsage(t *testing.T, pool *pgxpool.Pool, userID, month string, count int) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO ai_usage_monthly (id, user_id, month, request_count) VALUES ($1, $2, $3, $4)`,
		uuid.New().String(), userID, month, count,
	)
	if err != nil {
		t.Fatalf("seedUsage: %v", err)
	}
}

func requireAppErrInt(t *testing.T, err error) *apperror.AppError {
	t.Helper()
	ae, ok := err.(*apperror.AppError)
	if !ok {
		t.Fatalf("expected AppError, got %v", err)
	}
	return ae
}

func TestRepo_ListSessions_OrderByUpdatedAtDesc(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanAITestData(t, pool)
	t.Cleanup(func() { cleanAITestData(t, pool) })

	repo := ai.NewRepository(pool)
	ctx := context.Background()
	userID := seedUser(t, pool, "pro")

	// Insert three sessions; bump updated_at out of insert order.
	ids := make([]string, 3)
	for i := range ids {
		s, err := repo.CreateSession(ctx, ai.Session{ID: uuid.New().String(), UserID: userID})
		if err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
		ids[i] = s.ID
	}
	// Make ids[1] the most recently updated, ids[0] the least.
	now := time.Now()
	set := func(id string, ts time.Time) {
		if _, err := pool.Exec(ctx, `UPDATE ai_sessions SET updated_at = $1 WHERE id = $2`, ts, id); err != nil {
			t.Fatalf("bump updated_at: %v", err)
		}
	}
	set(ids[0], now.Add(-2*time.Hour))
	set(ids[2], now.Add(-1*time.Hour))
	set(ids[1], now)

	got, err := repo.ListSessions(ctx, userID)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	want := []string{ids[1], ids[2], ids[0]}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for i := range want {
		if got[i].ID != want[i] {
			t.Fatalf("order[%d] = %s, want %s", i, got[i].ID, want[i])
		}
	}
}

func TestRepo_ListSessions_RowIsolation(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanAITestData(t, pool)
	t.Cleanup(func() { cleanAITestData(t, pool) })

	repo := ai.NewRepository(pool)
	ctx := context.Background()
	owner := seedUser(t, pool, "free")
	other := seedUser(t, pool, "free")

	if _, err := repo.CreateSession(ctx, ai.Session{ID: uuid.New().String(), UserID: owner}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got, err := repo.ListSessions(ctx, other)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("other user saw %d sessions, want 0", len(got))
	}
}

func TestRepo_ListMessages_OrderByCreatedAtAsc(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanAITestData(t, pool)
	t.Cleanup(func() { cleanAITestData(t, pool) })

	repo := ai.NewRepository(pool)
	ctx := context.Background()
	userID := seedUser(t, pool, "pro")
	sess, err := repo.CreateSession(ctx, ai.Session{ID: uuid.New().String(), UserID: userID})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	base := time.Now().Add(-time.Hour)
	seedMessage(t, pool, sess.ID, "assistant", "second", base.Add(time.Minute))
	seedMessage(t, pool, sess.ID, "user", "first", base)

	msgs, err := repo.ListMessages(ctx, sess.ID)
	if err != nil {
		t.Fatalf("ListMessages: %v", err)
	}
	if len(msgs) != 2 || msgs[0].Content != "first" || msgs[1].Content != "second" {
		t.Fatalf("messages not ASC by created_at: %+v", msgs)
	}
}

func TestRepo_DeleteSession_CascadesMessages(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanAITestData(t, pool)
	t.Cleanup(func() { cleanAITestData(t, pool) })

	repo := ai.NewRepository(pool)
	ctx := context.Background()
	userID := seedUser(t, pool, "pro")
	sess, err := repo.CreateSession(ctx, ai.Session{ID: uuid.New().String(), UserID: userID})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	seedMessage(t, pool, sess.ID, "user", "hi", time.Now())

	if err := repo.DeleteSession(ctx, userID, sess.ID); err != nil {
		t.Fatalf("DeleteSession: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM ai_messages WHERE session_id = $1`, sess.ID).Scan(&count); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if count != 0 {
		t.Fatalf("messages after delete = %d, want 0 (cascade)", count)
	}
}

func TestRepo_GetSession_ForeignID_404(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanAITestData(t, pool)
	t.Cleanup(func() { cleanAITestData(t, pool) })

	repo := ai.NewRepository(pool)
	ctx := context.Background()
	owner := seedUser(t, pool, "free")
	other := seedUser(t, pool, "free")
	sess, err := repo.CreateSession(ctx, ai.Session{ID: uuid.New().String(), UserID: owner})
	if err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	_, err = repo.GetSession(ctx, other, sess.ID)
	ae := requireAppErrInt(t, err)
	if ae.Code != apperror.ErrResourceNotFound || ae.Status != http.StatusNotFound {
		t.Fatalf("got %s/%d, want RESOURCE_NOT_FOUND/404", ae.Code, ae.Status)
	}
}

func TestRepo_Usage(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanAITestData(t, pool)
	t.Cleanup(func() { cleanAITestData(t, pool) })

	repo := ai.NewRepository(pool)
	ctx := context.Background()
	userID := seedUser(t, pool, "pro")

	thisMonth := time.Now().UTC().Format("2006-01") + "-01"
	seedUsage(t, pool, userID, thisMonth, 7)
	seedUsage(t, pool, userID, "2000-01-01", 4)

	// Lifetime sum spans all rows.
	sum, err := repo.UsageSum(ctx, userID)
	if err != nil {
		t.Fatalf("UsageSum: %v", err)
	}
	if sum != 11 {
		t.Fatalf("UsageSum = %d, want 11", sum)
	}

	// Month lookup returns only this month.
	month, err := repo.UsageForMonth(ctx, userID, thisMonth)
	if err != nil {
		t.Fatalf("UsageForMonth: %v", err)
	}
	if month != 7 {
		t.Fatalf("UsageForMonth = %d, want 7", month)
	}

	// Absent month row → 0, no error.
	empty, err := repo.UsageForMonth(ctx, userID, "1999-01-01")
	if err != nil {
		t.Fatalf("UsageForMonth empty: %v", err)
	}
	if empty != 0 {
		t.Fatalf("UsageForMonth empty = %d, want 0", empty)
	}
}

// ── Reserve / Refund ──────────────────────────────────────────────────────────

func requireLimitReached(t *testing.T, err error) {
	t.Helper()
	ae := requireAppErrInt(t, err)
	if ae.Code != apperror.ErrAILimitReached || ae.Status != http.StatusTooManyRequests {
		t.Fatalf("got %s/%d, want AI_LIMIT_REACHED/429", ae.Code, ae.Status)
	}
}

func usageCount(t *testing.T, pool *pgxpool.Pool, userID string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(SUM(request_count), 0) FROM ai_usage_monthly WHERE user_id = $1`,
		userID,
	).Scan(&n)
	if err != nil {
		t.Fatalf("usageCount: %v", err)
	}
	return n
}

func TestRepo_ReserveMonthly(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanAITestData(t, pool)
	t.Cleanup(func() { cleanAITestData(t, pool) })

	repo := ai.NewRepository(pool)
	ctx := context.Background()
	userID := seedUser(t, pool, "pro")
	month := time.Now().UTC().Format("2006-01") + "-01"

	// First reserve inserts the month row at 1.
	id, err := repo.ReserveMonthly(ctx, userID, month, 3)
	if err != nil {
		t.Fatalf("ReserveMonthly first: %v", err)
	}
	if id == "" {
		t.Fatal("expected usage row id")
	}

	// Increment to the limit, then the guard must reject.
	for i := 0; i < 2; i++ {
		if _, err := repo.ReserveMonthly(ctx, userID, month, 3); err != nil {
			t.Fatalf("ReserveMonthly %d: %v", i+2, err)
		}
	}
	_, err = repo.ReserveMonthly(ctx, userID, month, 3)
	requireLimitReached(t, err)
	if n := usageCount(t, pool, userID); n != 3 {
		t.Fatalf("count after exhausted reserve = %d, want 3", n)
	}

	// A previous month's spend never blocks the current month.
	seedUsage(t, pool, userID, "2000-02-01", 3)
	if _, err := repo.ReserveMonthly(ctx, userID, month, 4); err != nil {
		t.Fatalf("prior months must not count toward the monthly guard: %v", err)
	}
}

func TestRepo_ReserveLifetime(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanAITestData(t, pool)
	t.Cleanup(func() { cleanAITestData(t, pool) })

	repo := ai.NewRepository(pool)
	ctx := context.Background()
	month := time.Now().UTC().Format("2006-01") + "-01"

	t.Run("increments up to the lifetime sum", func(t *testing.T) {
		userID := seedUser(t, pool, "free")
		for i := 0; i < 5; i++ {
			if _, err := repo.ReserveLifetime(ctx, userID, month, 5); err != nil {
				t.Fatalf("ReserveLifetime %d: %v", i+1, err)
			}
		}
		_, err := repo.ReserveLifetime(ctx, userID, month, 5)
		requireLimitReached(t, err)
		if n := usageCount(t, pool, userID); n != 5 {
			t.Fatalf("count = %d, want 5", n)
		}
	})

	t.Run("prior-month spend blocks a fresh month row", func(t *testing.T) {
		userID := seedUser(t, pool, "free")
		seedUsage(t, pool, userID, "2000-03-01", 5)

		// No current-month row exists — the INSERT path must honour the
		// lifetime sum too, not just the conflict path.
		_, err := repo.ReserveLifetime(ctx, userID, month, 5)
		requireLimitReached(t, err)
		if n := usageCount(t, pool, userID); n != 5 {
			t.Fatalf("count = %d, want 5 (fresh-month insert leaked past the guard)", n)
		}
	})

	t.Run("partial prior-month spend still reserves", func(t *testing.T) {
		userID := seedUser(t, pool, "free")
		seedUsage(t, pool, userID, "2000-03-01", 4)

		if _, err := repo.ReserveLifetime(ctx, userID, month, 5); err != nil {
			t.Fatalf("ReserveLifetime with 4/5 lifetime used: %v", err)
		}
		if n := usageCount(t, pool, userID); n != 5 {
			t.Fatalf("count = %d, want 5", n)
		}
	})
}

func TestRepo_RefundUsage(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanAITestData(t, pool)
	t.Cleanup(func() { cleanAITestData(t, pool) })

	repo := ai.NewRepository(pool)
	ctx := context.Background()
	userID := seedUser(t, pool, "pro")
	month := time.Now().UTC().Format("2006-01") + "-01"

	id, err := repo.ReserveMonthly(ctx, userID, month, 500)
	if err != nil {
		t.Fatalf("ReserveMonthly: %v", err)
	}

	if err := repo.RefundUsage(ctx, id); err != nil {
		t.Fatalf("RefundUsage: %v", err)
	}
	if n := usageCount(t, pool, userID); n != 0 {
		t.Fatalf("count after refund = %d, want 0", n)
	}

	// A second refund on the same row must not go negative.
	if err := repo.RefundUsage(ctx, id); err != nil {
		t.Fatalf("RefundUsage twice: %v", err)
	}
	if n := usageCount(t, pool, userID); n != 0 {
		t.Fatalf("count after double refund = %d, want 0 (never negative)", n)
	}

	// Refund frees a slot the guard then accepts again.
	if _, err := repo.ReserveMonthly(ctx, userID, month, 1); err != nil {
		t.Fatalf("reserve after refund: %v", err)
	}
	_, err = repo.ReserveMonthly(ctx, userID, month, 1)
	requireLimitReached(t, err)
}

// ── Concurrency — a parallel burst can never exceed the limit ─────────────────

func runBurst(t *testing.T, workers int, reserve func() error) (successes, limited int) {
	t.Helper()
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- reserve()
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		switch {
		case err == nil:
			successes++
		default:
			ae, ok := err.(*apperror.AppError)
			if !ok || ae.Code != apperror.ErrAILimitReached {
				t.Errorf("unexpected burst error: %v", err)
				continue
			}
			limited++
		}
	}
	return successes, limited
}

func TestRepo_ReserveMonthly_ConcurrentBurst(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanAITestData(t, pool)
	t.Cleanup(func() { cleanAITestData(t, pool) })

	repo := ai.NewRepository(pool)
	userID := seedUser(t, pool, "pro")
	month := time.Now().UTC().Format("2006-01") + "-01"
	const limit, workers = 5, 20

	successes, limited := runBurst(t, workers, func() error {
		_, err := repo.ReserveMonthly(context.Background(), userID, month, limit)
		return err
	})

	if successes != limit || limited != workers-limit {
		t.Fatalf("burst: %d successes / %d limited, want %d / %d", successes, limited, limit, workers-limit)
	}
	if n := usageCount(t, pool, userID); n != limit {
		t.Fatalf("count after burst = %d, want %d (limit exceeded under concurrency)", n, limit)
	}
}

func TestRepo_ReserveLifetime_ConcurrentBurst_NewUser(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanAITestData(t, pool)
	t.Cleanup(func() { cleanAITestData(t, pool) })

	repo := ai.NewRepository(pool)
	// Brand-new user: no usage rows exist, so there is no row lock to
	// serialize on — the hardest case for the lifetime guard.
	userID := seedUser(t, pool, "free")
	month := time.Now().UTC().Format("2006-01") + "-01"
	const limit, workers = 5, 20

	successes, limited := runBurst(t, workers, func() error {
		_, err := repo.ReserveLifetime(context.Background(), userID, month, limit)
		return err
	})

	if successes != limit || limited != workers-limit {
		t.Fatalf("burst: %d successes / %d limited, want %d / %d", successes, limited, limit, workers-limit)
	}
	if n := usageCount(t, pool, userID); n != limit {
		t.Fatalf("count after burst = %d, want %d (limit exceeded under concurrency)", n, limit)
	}
}
