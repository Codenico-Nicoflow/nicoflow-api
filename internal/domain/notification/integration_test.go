//go:build integration

package notification_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
)

func cleanNotificationTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	queries := []string{
		`DELETE FROM notifications WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@notif.integration.test')`,
		`DELETE FROM push_subscriptions WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@notif.integration.test')`,
		`DELETE FROM users WHERE email LIKE '%@notif.integration.test'`,
	}
	for _, q := range queries {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			t.Fatalf("cleanNotificationTestData: %v", err)
		}
	}
}

func seedUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, username, password_hash, plan)
		 VALUES ($1, $2, $3, 'x', 'free')`,
		id, id+"@notif.integration.test", "u_"+id[:8],
	)
	if err != nil {
		t.Fatalf("seedUser: %v", err)
	}
	return id
}

func newRepo(t *testing.T) (notification.Repository, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewTestDB(t)
	cleanNotificationTestData(t, pool)
	t.Cleanup(func() { cleanNotificationTestData(t, pool) })
	return notification.NewRepository(pool), pool
}

func insert(t *testing.T, r notification.Repository, userID string, dedupe *string) notification.Notification {
	t.Helper()
	n, inserted, err := r.InsertIfAbsent(context.Background(), notification.Notification{
		ID:        uuid.New().String(),
		UserID:    userID,
		Type:      notification.TypeMorningDigest,
		Title:     "Task due",
		DedupeKey: dedupe,
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	if !inserted {
		t.Fatalf("insert: expected a row to be inserted")
	}
	return n
}

func TestRepo_CountUnread_and_MarkAllRead(t *testing.T) {
	r, pool := newRepo(t)
	userID := seedUser(t, pool)

	for i := 0; i < 3; i++ {
		insert(t, r, userID, nil)
	}

	count, err := r.CountUnread(context.Background(), userID)
	if err != nil {
		t.Fatalf("CountUnread: %v", err)
	}
	if count != 3 {
		t.Fatalf("CountUnread = %d, want 3", count)
	}

	n, err := r.MarkAllRead(context.Background(), userID)
	if err != nil {
		t.Fatalf("MarkAllRead: %v", err)
	}
	if n != 3 {
		t.Fatalf("MarkAllRead = %d, want 3", n)
	}

	count, _ = r.CountUnread(context.Background(), userID)
	if count != 0 {
		t.Fatalf("CountUnread after mark-all = %d, want 0", count)
	}
}

func TestRepo_List_IsReadFilter_and_Cursor(t *testing.T) {
	r, pool := newRepo(t)
	userID := seedUser(t, pool)

	// Insert 3 unread; mark one read.
	n1 := insert(t, r, userID, nil)
	insert(t, r, userID, nil)
	insert(t, r, userID, nil)
	if _, err := r.MarkRead(context.Background(), userID, n1.ID); err != nil {
		t.Fatalf("MarkRead: %v", err)
	}

	// Filter unread only → 2.
	unread := false
	items, _, err := r.List(context.Background(), userID, notification.ListNotificationsFilter{IsRead: &unread})
	if err != nil {
		t.Fatalf("List unread: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("unread list = %d, want 2", len(items))
	}

	// Page size 2 → nextCursor set; page 2 → 1 remaining, empty cursor.
	page1, next, err := r.List(context.Background(), userID, notification.ListNotificationsFilter{Limit: 2})
	if err != nil {
		t.Fatalf("List page1: %v", err)
	}
	if len(page1) != 2 || next == "" {
		t.Fatalf("page1 = %d items, next=%q; want 2 items + a cursor", len(page1), next)
	}
	page2, next2, err := r.List(context.Background(), userID, notification.ListNotificationsFilter{Limit: 2, Cursor: next})
	if err != nil {
		t.Fatalf("List page2: %v", err)
	}
	if len(page2) != 1 || next2 != "" {
		t.Fatalf("page2 = %d items, next=%q; want 1 item + empty cursor", len(page2), next2)
	}
}

func TestRepo_InsertIfAbsent_Idempotent(t *testing.T) {
	r, pool := newRepo(t)
	userID := seedUser(t, pool)

	key := "task_due_soon:task-1:2026-07-14"
	insert(t, r, userID, &key)

	// Second insert with the same dedupe key is a no-op.
	_, inserted, err := r.InsertIfAbsent(context.Background(), notification.Notification{
		ID:        uuid.New().String(),
		UserID:    userID,
		Type:      notification.TypeMorningDigest,
		Title:     "Task due (dup)",
		DedupeKey: &key,
	})
	if err != nil {
		t.Fatalf("InsertIfAbsent dup: %v", err)
	}
	if inserted {
		t.Fatal("second insert with same dedupe key should be skipped")
	}

	count, _ := r.CountUnread(context.Background(), userID)
	if count != 1 {
		t.Fatalf("CountUnread = %d, want 1 (dedupe held)", count)
	}
}

func TestRepo_RowIsolation(t *testing.T) {
	r, pool := newRepo(t)
	owner := seedUser(t, pool)
	other := seedUser(t, pool)

	n := insert(t, r, owner, nil)

	// Another user cannot mark it read.
	_, err := r.MarkRead(context.Background(), other, n.ID)
	if ae := asApp(err); ae == nil || ae.Code != apperror.ErrNotificationNotFound {
		t.Fatalf("MarkRead cross-user err = %v, want NOTIFICATION_NOT_FOUND", err)
	}

	// Another user cannot delete it.
	err = r.Delete(context.Background(), other, n.ID)
	if ae := asApp(err); ae == nil || ae.Code != apperror.ErrNotificationNotFound {
		t.Fatalf("Delete cross-user err = %v, want NOTIFICATION_NOT_FOUND", err)
	}

	// Owner still can.
	if err := r.Delete(context.Background(), owner, n.ID); err != nil {
		t.Fatalf("owner Delete: %v", err)
	}
}

func asApp(err error) *apperror.AppError {
	var ae *apperror.AppError
	if errors.As(err, &ae) {
		return ae
	}
	return nil
}
