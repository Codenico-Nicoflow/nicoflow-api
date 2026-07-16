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
	if !p.EmailDigest || p.PushEnabled || p.SMSEnabled ||
		p.BeforeDueMinutes != 1440 || p.AfterDueMinutes != 0 {
		t.Fatalf("defaults = %+v, want emailDigest=true push=false sms=false before=1440 after=0", p)
	}
	// Per-family toggles (NIC-1591) default on.
	if !p.OverdueEnabled || !p.DailySummaryEnabled || !p.InboxNudgesEnabled || !p.StreaksEnabled {
		t.Fatalf("family defaults = %+v, want all four enabled", p)
	}
}

// NIC-1591: per-family toggles persist and untouched families keep their value.
func TestRepo_UpsertPreferences_FamilyToggles(t *testing.T) {
	r, pool := newPrefsRepo(t)
	userID := seedUser(t, pool)

	off := false
	p, err := r.UpsertPreferences(context.Background(), userID, notification.UpdatePreferences{
		OverdueEnabled: &off,
		StreaksEnabled: &off,
	})
	if err != nil {
		t.Fatalf("UpsertPreferences: %v", err)
	}
	if p.OverdueEnabled || p.StreaksEnabled {
		t.Fatalf("toggled = %+v, want overdue=false streaks=false", p)
	}
	// Untouched families stay on.
	if !p.DailySummaryEnabled || !p.InboxNudgesEnabled {
		t.Fatalf("untouched = %+v, want dailySummary=true inbox=true", p)
	}

	got, err := r.GetPreferences(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetPreferences after upsert: %v", err)
	}
	if got.OverdueEnabled || got.StreaksEnabled || !got.DailySummaryEnabled || !got.InboxNudgesEnabled {
		t.Fatalf("stored = %+v, want overdue=false streaks=false dailySummary=true inbox=true", got)
	}
}

func TestRepo_UpsertPreferences_CreatesWithDefaultsForAbsentFields(t *testing.T) {
	r, pool := newPrefsRepo(t)
	userID := seedUser(t, pool)

	// Partial create: only emailDigest + beforeDueMinutes; others take defaults.
	digest := false
	before := 60
	p, err := r.UpsertPreferences(context.Background(), userID, notification.UpdatePreferences{
		EmailDigest:      &digest,
		BeforeDueMinutes: &before,
	})
	if err != nil {
		t.Fatalf("UpsertPreferences create: %v", err)
	}
	if p.EmailDigest || p.BeforeDueMinutes != 60 {
		t.Fatalf("create = %+v, want emailDigest=false before=60", p)
	}
	if p.PushEnabled || p.SMSEnabled || p.AfterDueMinutes != 0 {
		t.Fatalf("create absent fields = %+v, want push=false sms=false after=0 (defaults)", p)
	}

	// Confirm it persisted (read path returns the stored row, not defaults).
	got, err := r.GetPreferences(context.Background(), userID)
	if err != nil {
		t.Fatalf("GetPreferences after create: %v", err)
	}
	if got.EmailDigest || got.BeforeDueMinutes != 60 {
		t.Fatalf("stored = %+v, want emailDigest=false before=60", got)
	}
}

func TestRepo_UpsertPreferences_UpdatesPreservingUntouchedFields(t *testing.T) {
	r, pool := newPrefsRepo(t)
	userID := seedUser(t, pool)

	// Create with a non-default beforeDueMinutes.
	before := 120
	if _, err := r.UpsertPreferences(context.Background(), userID,
		notification.UpdatePreferences{BeforeDueMinutes: &before}); err != nil {
		t.Fatalf("UpsertPreferences seed: %v", err)
	}

	// Update only pushEnabled — beforeDueMinutes must survive.
	push := true
	p, err := r.UpsertPreferences(context.Background(), userID,
		notification.UpdatePreferences{PushEnabled: &push})
	if err != nil {
		t.Fatalf("UpsertPreferences update: %v", err)
	}
	if !p.PushEnabled {
		t.Fatalf("pushEnabled = false, want true")
	}
	if p.BeforeDueMinutes != 120 {
		t.Fatalf("beforeDueMinutes = %d, want 120 (preserved)", p.BeforeDueMinutes)
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
