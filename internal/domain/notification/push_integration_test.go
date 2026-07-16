//go:build integration

package notification_test

import (
	"context"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

// AC1: subscribe stores a row; a repeat subscribe on the same endpoint upserts
// (refreshes keys) rather than duplicating. Row-scoped by user.
func TestPushRepo_UpsertAndList(t *testing.T) {
	r, pool := newRepo(t)
	userID := seedUser(t, pool)
	ctx := context.Background()

	sub := notification.PushSubscription{Endpoint: "https://push/1", P256dhKey: "p1", AuthKey: "a1", UserAgent: "UA"}
	if err := r.UpsertPushSubscription(ctx, userID, sub); err != nil {
		t.Fatalf("UpsertPushSubscription: %v", err)
	}
	// Same endpoint, refreshed keys → upsert, not a second row.
	sub.P256dhKey = "p2"
	if err := r.UpsertPushSubscription(ctx, userID, sub); err != nil {
		t.Fatalf("UpsertPushSubscription (refresh): %v", err)
	}

	subs, err := r.ListPushSubscriptions(ctx, userID)
	if err != nil {
		t.Fatalf("ListPushSubscriptions: %v", err)
	}
	if len(subs) != 1 || subs[0].P256dhKey != "p2" {
		t.Fatalf("want one row with refreshed key, got %+v", subs)
	}
}

// Row isolation: one user's subscriptions are never returned for another.
func TestPushRepo_RowIsolation(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	u1 := seedUser(t, pool)
	u2 := seedUser(t, pool)

	if err := r.UpsertPushSubscription(ctx, u1, notification.PushSubscription{Endpoint: "https://push/u1", P256dhKey: "p", AuthKey: "a"}); err != nil {
		t.Fatalf("upsert u1: %v", err)
	}

	subs, err := r.ListPushSubscriptions(ctx, u2)
	if err != nil {
		t.Fatalf("list u2: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("u2 must not see u1's subscriptions, got %+v", subs)
	}
}

// Delete by endpoint is idempotent and row-scoped.
func TestPushRepo_Delete(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)

	if err := r.UpsertPushSubscription(ctx, userID, notification.PushSubscription{Endpoint: "https://push/1", P256dhKey: "p", AuthKey: "a"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := r.DeletePushSubscription(ctx, userID, "https://push/1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	// Second delete is a no-op, not an error.
	if err := r.DeletePushSubscription(ctx, userID, "https://push/1"); err != nil {
		t.Fatalf("idempotent delete: %v", err)
	}

	subs, err := r.ListPushSubscriptions(ctx, userID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("want no rows after delete, got %+v", subs)
	}
}
