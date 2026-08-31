package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

// at0800UTC is a fixed clock at 08:00 UTC — the reminder hour for a UTC user.
func at0800UTC() time.Time { return time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) }

func remindableUser(userID string) RemindableUser {
	return RemindableUser{UserID: userID, Timezone: "UTC", Plan: "free", MorningDigestEnabled: true, MorningHour: 8}
}

// N>0 across scheduled/overdue/unprocessed → one morning_digest with all three
// counts in metadata. Free plan fires — the digest is unified across plans.
func TestDayStartRun_FiresDigestWithAllCounts(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{remindableUser("u1")},
		scheduled:   map[string]int{"u1": 3},
		overdue:     map[string]int{"u1": 1},
		unprocessed: map[string]int{"u1": 2},
	}
	creator := &fakeCreator{inserted: true}
	n := NewDayStartNotifier(repo, creator)
	n.now = at0800UTC

	generated, err := n.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if generated.Fired != 1 || len(creator.calls) != 1 {
		t.Fatalf("generated=%d calls=%d, want 1/1", generated.Fired, len(creator.calls))
	}
	got := creator.calls[0]
	if got.Type != notification.TypeMorningDigest {
		t.Fatalf("type = %q, want morning_digest", got.Type)
	}
	if got.DedupeKey == nil || *got.DedupeKey != "morning_digest:u1:2026-07-14" {
		t.Fatalf("dedupe = %v, want morning_digest:u1:2026-07-14", got.DedupeKey)
	}
	var meta struct {
		Scheduled   int `json:"scheduled"`
		Overdue     int `json:"overdue"`
		Unprocessed int `json:"unprocessed"`
	}
	if err := json.Unmarshal(got.Metadata, &meta); err != nil {
		t.Fatalf("metadata unmarshal: %v", err)
	}
	if meta.Scheduled != 3 || meta.Overdue != 1 || meta.Unprocessed != 2 {
		t.Fatalf("metadata = %+v, want scheduled=3 overdue=1 unprocessed=2", meta)
	}
}

// All three counts zero → true-empty state, stay silent.
func TestDayStartRun_SilentWhenAllZero(t *testing.T) {
	repo := &fakeRepo{users: []RemindableUser{remindableUser("u1")}}
	creator := &fakeCreator{inserted: true}
	n := NewDayStartNotifier(repo, creator)
	n.now = at0800UTC

	if generated, _ := n.Run(context.Background(), false); generated.Fired != 0 || len(creator.calls) != 0 {
		t.Fatalf("all-zero must stay silent, got generated=%d calls=%d", generated.Fired, len(creator.calls))
	}
}

// Any single count alone is enough to fire.
func TestDayStartRun_FiresOnAnySingleCount(t *testing.T) {
	tests := []struct {
		name string
		repo *fakeRepo
	}{
		{"scheduled only", &fakeRepo{users: []RemindableUser{remindableUser("u1")}, scheduled: map[string]int{"u1": 1}}},
		{"overdue only", &fakeRepo{users: []RemindableUser{remindableUser("u1")}, overdue: map[string]int{"u1": 1}}},
		{"unprocessed only", &fakeRepo{users: []RemindableUser{remindableUser("u1")}, unprocessed: map[string]int{"u1": 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			creator := &fakeCreator{inserted: true}
			n := NewDayStartNotifier(tt.repo, creator)
			n.now = at0800UTC
			if generated, _ := n.Run(context.Background(), false); generated.Fired != 1 {
				t.Fatalf("want 1 fired, got %d", generated.Fired)
			}
		})
	}
}

// A user with the morning digest toggled off gets nothing, even with counts.
func TestDayStartRun_ToggleOffSkips(t *testing.T) {
	u := remindableUser("u1")
	u.MorningDigestEnabled = false
	repo := &fakeRepo{
		users:     []RemindableUser{u},
		scheduled: map[string]int{"u1": 5},
	}
	creator := &fakeCreator{inserted: true}
	n := NewDayStartNotifier(repo, creator)
	n.now = at0800UTC

	if generated, _ := n.Run(context.Background(), false); generated.Fired != 0 || len(creator.calls) != 0 {
		t.Fatalf("toggled-off user must get nothing, got generated=%d calls=%d", generated.Fired, len(creator.calls))
	}
}

// AC3: idempotent — a dedupe-held row is attempted but not counted as fired.
func TestDayStartRun_Idempotent(t *testing.T) {
	repo := &fakeRepo{
		users:     []RemindableUser{remindableUser("u1")},
		scheduled: map[string]int{"u1": 1},
	}
	creator := &fakeCreator{inserted: false} // dedupe held
	n := NewDayStartNotifier(repo, creator)
	n.now = at0800UTC

	generated, err := n.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if generated.Fired != 0 || len(creator.calls) != 1 {
		t.Fatalf("want 0 generated + 1 attempt, got generated=%d calls=%d", generated.Fired, len(creator.calls))
	}
}

func TestDayStartRun_SkipsUsersNotAtReminderHour(t *testing.T) {
	repo := &fakeRepo{
		users:     []RemindableUser{remindableUser("u1")},
		scheduled: map[string]int{"u1": 1},
	}
	creator := &fakeCreator{inserted: true}
	n := NewDayStartNotifier(repo, creator)
	n.now = func() time.Time { return time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC) } // 12:00 UTC

	if generated, _ := n.Run(context.Background(), false); generated.Fired != 0 || len(creator.calls) != 0 {
		t.Fatalf("want 0/0 outside reminder hour, got %d/%d", generated.Fired, len(creator.calls))
	}
}

// Per-user isolation: one user's repo error must not abort the batch.
func TestDayStartRun_PerUserIsolation(t *testing.T) {
	repo := &fakeRepo{
		users:        []RemindableUser{remindableUser("u1"), remindableUser("u2")},
		scheduledErr: map[string]error{"u1": errors.New("db down for u1")},
		scheduled:    map[string]int{"u2": 1},
	}
	creator := &fakeCreator{inserted: true}
	n := NewDayStartNotifier(repo, creator)
	n.now = at0800UTC

	generated, err := n.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run must not return a per-user error: %v", err)
	}
	if generated.Fired != 1 || len(creator.calls) != 1 || creator.calls[0].UserID != "u2" {
		t.Fatalf("want u2's digest despite u1 failing, got generated=%d calls=%d", generated.Fired, len(creator.calls))
	}
}
