//go:build integration

package ai_test

import (
	"context"
	"net/http"
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
