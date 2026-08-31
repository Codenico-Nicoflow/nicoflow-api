//go:build integration

package notification_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

// cleanPrefsTestData removes preference rows for the integration test users. Rows
// cascade on user delete, but drop them explicitly so a rerun starts clean even
// if a prior run left the user around.
func cleanPrefsTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`DELETE FROM notification_preferences WHERE user_id IN
		   (SELECT id FROM users WHERE email LIKE '%@notif.integration.test')`)
	if err != nil {
		t.Fatalf("cleanPrefsTestData: %v", err)
	}
}

func newPrefsRepo(t *testing.T) (notification.Repository, *pgxpool.Pool) {
	t.Helper()
	r, pool := newRepo(t)
	cleanPrefsTestData(t, pool)
	t.Cleanup(func() { cleanPrefsTestData(t, pool) })
	return r, pool
}

func TestRepo_GetPreferences_DefaultsWhenAbsent(t *testing.T) {
	r, pool := newPrefsRepo(t)
	userID := seedUser(t, pool)

	p, err := r.GetPreferences(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetPreferences: %v", err)
	}
	if !p.EmailDigest || p.PushEnabled || p.SMSEnabled {
		t.Fatalf("defaults = %+v, want emailDigest=true push=false sms=false", p)
	}
	// Digest toggles + streaks default on.
	if !p.MorningDigestEnabled || !p.EveningDigestEnabled || !p.StreaksEnabled {
		t.Fatalf("digest defaults = %+v, want all three enabled", p)
	}
}

// Digest toggles persist and an untouched toggle keeps its value.
func TestRepo_UpsertPreferences_DigestToggles(t *testing.T) {
	r, pool := newPrefsRepo(t)
	userID := seedUser(t, pool)

	off := false
	p, err := r.UpsertPreferences(context.Background(), userID, notification.UpdatePreferences{
		MorningDigestEnabled: &off,
		StreaksEnabled:       &off,
	})
	if err != nil {
		t.Fatalf("UpsertPreferences: %v", err)
	}
	if p.MorningDigestEnabled || p.StreaksEnabled {
		t.Fatalf("toggled = %+v, want morningDigest=false streaks=false", p)
	}
	// Untouched toggle stays on.
	if !p.EveningDigestEnabled {
		t.Fatalf("untouched = %+v, want eveningDigest=true", p)
	}

	got, err := r.GetPreferences(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetPreferences after upsert: %v", err)
	}
	if got.MorningDigestEnabled || got.StreaksEnabled || !got.EveningDigestEnabled {
		t.Fatalf("stored = %+v, want morningDigest=false streaks=false eveningDigest=true", got)
	}
}

func TestRepo_UpsertPreferences_CreatesWithDefaultsForAbsentFields(t *testing.T) {
	r, pool := newPrefsRepo(t)
	userID := seedUser(t, pool)

	// Partial create: only emailDigest + eveningDigestEnabled; others take defaults.
	digest := false
	evening := false
	p, err := r.UpsertPreferences(context.Background(), userID, notification.UpdatePreferences{
		EmailDigest:          &digest,
		EveningDigestEnabled: &evening,
	})
	if err != nil {
		t.Fatalf("UpsertPreferences create: %v", err)
	}
	if p.EmailDigest || p.EveningDigestEnabled {
		t.Fatalf("create = %+v, want emailDigest=false eveningDigest=false", p)
	}
	if p.PushEnabled || p.SMSEnabled || !p.MorningDigestEnabled {
		t.Fatalf("create absent fields = %+v, want push=false sms=false morningDigest=true (defaults)", p)
	}

	// Confirm it persisted (read path returns the stored row, not defaults).
	got, err := r.GetPreferences(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetPreferences after create: %v", err)
	}
	if got.EmailDigest || got.EveningDigestEnabled {
		t.Fatalf("stored = %+v, want emailDigest=false eveningDigest=false", got)
	}
}

func TestRepo_UpsertPreferences_UpdatesPreservingUntouchedFields(t *testing.T) {
	r, pool := newPrefsRepo(t)
	userID := seedUser(t, pool)

	// Create with a non-default eveningDigestEnabled.
	evening := false
	if _, err := r.UpsertPreferences(context.Background(), userID,
		notification.UpdatePreferences{EveningDigestEnabled: &evening}); err != nil {
		t.Fatalf("UpsertPreferences seed: %v", err)
	}

	// Update only pushEnabled — eveningDigestEnabled must survive.
	push := true
	p, err := r.UpsertPreferences(context.Background(), userID,
		notification.UpdatePreferences{PushEnabled: &push})
	if err != nil {
		t.Fatalf("UpsertPreferences update: %v", err)
	}
	if !p.PushEnabled {
		t.Fatalf("pushEnabled = false, want true")
	}
	if p.EveningDigestEnabled {
		t.Fatalf("eveningDigestEnabled = true, want false (preserved)")
	}
}

func TestRepo_UpsertPreferences_RowScope(t *testing.T) {
	r, pool := newPrefsRepo(t)
	owner := seedUser(t, pool)
	other := seedUser(t, pool)

	digest := false
	if _, err := r.UpsertPreferences(context.Background(), owner,
		notification.UpdatePreferences{EmailDigest: &digest}); err != nil {
		t.Fatalf("owner upsert: %v", err)
	}

	// The other user is unaffected — still gets defaults.
	p, err := r.GetPreferences(context.Background(), other)
	if err != nil {
		t.Fatalf("other GetPreferences: %v", err)
	}
	if !p.EmailDigest {
		t.Fatalf("other user emailDigest = false, want true (isolated defaults)")
	}
}
