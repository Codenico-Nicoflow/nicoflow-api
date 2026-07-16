package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

func TestInboxLocalTime(t *testing.T) {
	at05UTC := time.Date(2026, 7, 14, 5, 0, 0, 0, time.UTC) // 08:00 Asia/Jerusalem
	tests := []struct {
		name   string
		now    time.Time
		tz     string
		wantOK bool
	}{
		{"local 08:00 → fire", at05UTC, "Asia/Jerusalem", true},
		{"UTC 08:00 → fire", time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC), "UTC", true},
		{"local 09:00 → skip", time.Date(2026, 7, 14, 6, 0, 0, 0, time.UTC), "Asia/Jerusalem", false},
		{"bad timezone → skip", at05UTC, "Not/AZone", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := inboxLocalTime(tt.now, tt.tz)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
		})
	}
}

func TestISOWeek(t *testing.T) {
	// 2026-07-14 is in ISO week 29.
	got := isoWeek(time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC))
	if got != "2026-W29" {
		t.Fatalf("isoWeek = %q, want 2026-W29", got)
	}
}

func at0800UTCInbox() time.Time { return time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) }

// AC1: Pro user, unprocessed count >= threshold → one inbox_unprocessed with count.
func TestInboxRun_UnprocessedNudge(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: planPro}},
		unprocessed: map[string]int{"u1": inboxUnprocessedThreshold},
	}
	creator := &fakeCreator{inserted: true}
	n := NewInboxNotifier(repo, creator)
	n.now = at0800UTCInbox

	generated, err := n.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if generated != 1 || len(creator.calls) != 1 {
		t.Fatalf("generated=%d calls=%d, want 1/1", generated, len(creator.calls))
	}
	got := creator.calls[0]
	if got.Type != notification.TypeInboxUnprocessed ||
		got.DedupeKey == nil || *got.DedupeKey != "inbox_unprocessed:u1:2026-07-14" {
		t.Fatalf("notification = %+v, want inbox_unprocessed + dedupe inbox_unprocessed:u1:2026-07-14", got)
	}
	var meta struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(got.Metadata, &meta); err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if meta.Count != inboxUnprocessedThreshold {
		t.Fatalf("count = %d, want %d", meta.Count, inboxUnprocessedThreshold)
	}
}

// AC2 (NIC-1591): a Pro user who disabled inbox nudges gets none, even over threshold.
func TestInboxRun_RespectsFamilyToggle(t *testing.T) {
	repo := &fakeRepo{
		familiesOff: true, // inbox_nudges_enabled = false for u1
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: planPro}},
		unprocessed: map[string]int{"u1": inboxUnprocessedThreshold},
		staleByID:   map[string]bool{"u1": true},
	}
	creator := &fakeCreator{inserted: true}
	n := NewInboxNotifier(repo, creator)
	n.now = at0800UTCInbox

	if generated, _ := n.Run(context.Background()); generated != 0 || len(creator.calls) != 0 {
		t.Fatalf("want 0/0 when inbox nudges disabled, got %d/%d", generated, len(creator.calls))
	}
}

// AC1: below threshold → no unprocessed nudge.
func TestInboxRun_BelowThresholdNoNudge(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: planPro}},
		unprocessed: map[string]int{"u1": inboxUnprocessedThreshold - 1},
	}
	creator := &fakeCreator{inserted: true}
	n := NewInboxNotifier(repo, creator)
	n.now = at0800UTCInbox

	if generated, _ := n.Run(context.Background()); generated != 0 || len(creator.calls) != 0 {
		t.Fatalf("below threshold → nothing, got generated=%d calls=%d", generated, len(creator.calls))
	}
}

// AC1/Pro gate: a free user at/above threshold gets nothing (both types Pro).
func TestInboxRun_FreeUserSkipped(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: "free"}},
		unprocessed: map[string]int{"u1": inboxUnprocessedThreshold + 10},
		staleByID:   map[string]bool{"u1": true},
	}
	creator := &fakeCreator{inserted: true}
	n := NewInboxNotifier(repo, creator)
	n.now = at0800UTCInbox

	if generated, _ := n.Run(context.Background()); generated != 0 || len(creator.calls) != 0 {
		t.Fatalf("free user must be skipped, got generated=%d calls=%d", generated, len(creator.calls))
	}
}

// AC2: Pro user with a stale capture → one inbox_stale deduped per ISO week.
func TestInboxRun_StaleWarning(t *testing.T) {
	repo := &fakeRepo{
		users:     []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: planPro}},
		staleByID: map[string]bool{"u1": true},
	}
	creator := &fakeCreator{inserted: true}
	n := NewInboxNotifier(repo, creator)
	n.now = at0800UTCInbox

	generated, err := n.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if generated != 1 || len(creator.calls) != 1 {
		t.Fatalf("generated=%d calls=%d, want 1/1", generated, len(creator.calls))
	}
	got := creator.calls[0]
	if got.Type != notification.TypeInboxStale ||
		got.DedupeKey == nil || *got.DedupeKey != "inbox_stale:u1:2026-W29" {
		t.Fatalf("notification = %+v, want inbox_stale + dedupe inbox_stale:u1:2026-W29", got)
	}
}

// Both outputs fire together when both conditions hold.
func TestInboxRun_BothOutputs(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: planPro}},
		unprocessed: map[string]int{"u1": inboxUnprocessedThreshold},
		staleByID:   map[string]bool{"u1": true},
	}
	creator := &fakeCreator{inserted: true}
	n := NewInboxNotifier(repo, creator)
	n.now = at0800UTCInbox

	if generated, _ := n.Run(context.Background()); generated != 2 || len(creator.calls) != 2 {
		t.Fatalf("want both outputs, got generated=%d calls=%d", generated, len(creator.calls))
	}
}

// AC3: idempotent — dedupe-held rows are attempted but not counted.
func TestInboxRun_Idempotent(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: planPro}},
		unprocessed: map[string]int{"u1": inboxUnprocessedThreshold},
		staleByID:   map[string]bool{"u1": true},
	}
	creator := &fakeCreator{inserted: false} // dedupe held
	n := NewInboxNotifier(repo, creator)
	n.now = at0800UTCInbox

	if generated, _ := n.Run(context.Background()); generated != 0 || len(creator.calls) != 2 {
		t.Fatalf("want 0 generated + 2 attempts, got generated=%d calls=%d", generated, len(creator.calls))
	}
}

func TestInboxRun_SkipsUsersNotAtReminderHour(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: planPro}},
		unprocessed: map[string]int{"u1": inboxUnprocessedThreshold},
	}
	creator := &fakeCreator{inserted: true}
	n := NewInboxNotifier(repo, creator)
	n.now = func() time.Time { return time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC) }

	if generated, _ := n.Run(context.Background()); generated != 0 || len(creator.calls) != 0 {
		t.Fatalf("want 0/0 outside reminder hour, got %d/%d", generated, len(creator.calls))
	}
}

// Per-user isolation: one user's count error must not abort the batch.
func TestInboxRun_PerUserIsolation(t *testing.T) {
	repo := &fakeRepo{
		users: []RemindableUser{
			{UserID: "u1", Timezone: "UTC", Plan: planPro},
			{UserID: "u2", Timezone: "UTC", Plan: planPro},
		},
		unprocErr:   map[string]error{"u1": errors.New("db down for u1")},
		unprocessed: map[string]int{"u2": inboxUnprocessedThreshold},
	}
	creator := &fakeCreator{inserted: true}
	n := NewInboxNotifier(repo, creator)
	n.now = at0800UTCInbox

	generated, err := n.Run(context.Background())
	if err != nil {
		t.Fatalf("Run must not return a per-user error: %v", err)
	}
	if generated != 1 || len(creator.calls) != 1 || creator.calls[0].UserID != "u2" {
		t.Fatalf("want u2's nudge despite u1 failing, got generated=%d calls=%d", generated, len(creator.calls))
	}
}
