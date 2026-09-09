//go:build integration

package notification_test

import (
	"context"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

func expoSub(token, deviceID string) notification.PushSubscription {
	return notification.PushSubscription{
		Platform:      notification.PlatformExpo,
		ExpoPushToken: token,
		DeviceID:      deviceID,
	}
}

// A device that re-registers gets a fresh Expo token, so device_id is the stable
// identity — the row must be updated in place rather than accumulating one dead
// row per reinstall.
func TestPushRepo_ExpoUpsertsByDeviceID(t *testing.T) {
	r, pool := newRepo(t)
	userID := seedUser(t, pool)
	ctx := context.Background()

	if err := r.UpsertPushSubscription(ctx, userID, expoSub("token-old", "device-1")); err != nil {
		t.Fatalf("UpsertPushSubscription: %v", err)
	}
	if err := r.UpsertPushSubscription(ctx, userID, expoSub("token-new", "device-1")); err != nil {
		t.Fatalf("UpsertPushSubscription (rotated token): %v", err)
	}

	subs, err := r.ListPushSubscriptions(ctx, userID)
	if err != nil {
		t.Fatalf("ListPushSubscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("want one row after a token rotation, got %d: %+v", len(subs), subs)
	}
	if subs[0].ExpoPushToken != "token-new" || subs[0].Platform != notification.PlatformExpo {
		t.Fatalf("want the refreshed expo row, got %+v", subs[0])
	}
}

// Without a device_id the token is the only identity available, so it keys the
// upsert instead.
func TestPushRepo_ExpoUpsertsByTokenWhenNoDeviceID(t *testing.T) {
	r, pool := newRepo(t)
	userID := seedUser(t, pool)
	ctx := context.Background()

	if err := r.UpsertPushSubscription(ctx, userID, expoSub("token-1", "")); err != nil {
		t.Fatalf("UpsertPushSubscription: %v", err)
	}
	if err := r.UpsertPushSubscription(ctx, userID, expoSub("token-1", "")); err != nil {
		t.Fatalf("UpsertPushSubscription (repeat): %v", err)
	}

	subs, err := r.ListPushSubscriptions(ctx, userID)
	if err != nil {
		t.Fatalf("ListPushSubscriptions: %v", err)
	}
	if len(subs) != 1 {
		t.Fatalf("a repeat subscribe must not duplicate, got %d: %+v", len(subs), subs)
	}
}

// Web and expo rows coexist for the same user — a browser and a phone are two
// independent destinations, and the fanout reads both.
func TestPushRepo_WebAndExpoCoexist(t *testing.T) {
	r, pool := newRepo(t)
	userID := seedUser(t, pool)
	ctx := context.Background()

	web := notification.PushSubscription{
		Platform: notification.PlatformWeb, Endpoint: "https://push/1", P256dhKey: "p", AuthKey: "a",
	}
	if err := r.UpsertPushSubscription(ctx, userID, web); err != nil {
		t.Fatalf("UpsertPushSubscription (web): %v", err)
	}
	if err := r.UpsertPushSubscription(ctx, userID, expoSub("token-1", "device-1")); err != nil {
		t.Fatalf("UpsertPushSubscription (expo): %v", err)
	}

	subs, err := r.ListPushSubscriptions(ctx, userID)
	if err != nil {
		t.Fatalf("ListPushSubscriptions: %v", err)
	}
	if len(subs) != 2 {
		t.Fatalf("want both rows, got %d: %+v", len(subs), subs)
	}

	// Deleting the expo row must leave the web row untouched.
	if err := r.DeleteExpoPushSubscription(ctx, userID, "token-1"); err != nil {
		t.Fatalf("DeleteExpoPushSubscription: %v", err)
	}
	subs, err = r.ListPushSubscriptions(ctx, userID)
	if err != nil {
		t.Fatalf("ListPushSubscriptions after delete: %v", err)
	}
	if len(subs) != 1 || subs[0].Platform != notification.PlatformWeb {
		t.Fatalf("want only the web row left, got %+v", subs)
	}
}

// Idempotent, like its web counterpart.
func TestPushRepo_DeleteExpoIsIdempotent(t *testing.T) {
	r, pool := newRepo(t)
	userID := seedUser(t, pool)
	ctx := context.Background()

	if err := r.DeleteExpoPushSubscription(ctx, userID, "never-stored"); err != nil {
		t.Fatalf("deleting a missing token must not error: %v", err)
	}
}
