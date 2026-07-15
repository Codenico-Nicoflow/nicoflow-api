package jobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

func TestOverdueLocalToday(t *testing.T) {
	// 2026-07-14 05:00 UTC = 08:00 Asia/Jerusalem (UTC+3 summer) → reminder hour.
	at05UTC := time.Date(2026, 7, 14, 5, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		now      time.Time
		tz       string
		wantDate string
		wantOK   bool
	}{
		{"local 08:00 → local today", at05UTC, "Asia/Jerusalem", "2026-07-14", true},
		{"UTC user at 08:00 → today", time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC), "UTC", "2026-07-14", true},
		{"local 09:00 → skip", time.Date(2026, 7, 14, 6, 0, 0, 0, time.UTC), "Asia/Jerusalem", "", false},
		{"bad timezone → skip", at05UTC, "Not/AZone", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := overdueLocalToday(tt.now, tt.tz)
			if ok != tt.wantOK || got != tt.wantDate {
				t.Fatalf("got (%q,%v), want (%q,%v)", got, ok, tt.wantDate, tt.wantOK)
			}
		})
	}
}

// AC1: overdue detected at local morning → one task_overdue per task.
func TestOverdueRun_FiresAtLocalMorning(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC"}},
		overdueByID: map[string][]DueTask{"u1": {{ID: "t1", Title: "Late report"}}},
	}
	creator := &fakeCreator{inserted: true}
	n := NewOverdueNotifier(repo, creator)
	n.now = func() time.Time { return time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) }

	generated, err := n.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if generated != 1 || len(creator.calls) != 1 {
		t.Fatalf("generated=%d calls=%d, want 1/1", generated, len(creator.calls))
	}
	got := creator.calls[0]
	if got.Type != notification.TypeTaskOverdue || got.DedupeKey == nil ||
		*got.DedupeKey != "task_overdue:t1:2026-07-14" {
		t.Fatalf("notification = %+v, want task_overdue + dedupe task_overdue:t1:2026-07-14", got)
	}
}

// Fires for all plans (FREE): a free user still gets the reminder.
func TestOverdueRun_FreePlanStillFires(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: "free"}},
		overdueByID: map[string][]DueTask{"u1": {{ID: "t1", Title: "Late"}}},
	}
	creator := &fakeCreator{inserted: true}
	n := NewOverdueNotifier(repo, creator)
	n.now = func() time.Time { return time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) }
	if generated, _ := n.Run(context.Background()); generated != 1 {
		t.Fatalf("generated = %d, want 1 (free plan must still fire)", generated)
	}
}

// AC2: idempotent within the day — dedupe-held rows are not counted.
func TestOverdueRun_DuplicateNotCounted(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC"}},
		overdueByID: map[string][]DueTask{"u1": {{ID: "t1", Title: "Late"}}},
	}
	creator := &fakeCreator{inserted: false} // dedupe held
	n := NewOverdueNotifier(repo, creator)
	n.now = func() time.Time { return time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) }

	generated, err := n.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if generated != 0 || len(creator.calls) != 1 {
		t.Fatalf("generated=%d calls=%d, want 0/1 (duplicate skipped, attempt made)", generated, len(creator.calls))
	}
}

func TestOverdueRun_SkipsUsersNotAtReminderHour(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC"}},
		overdueByID: map[string][]DueTask{"u1": {{ID: "t1", Title: "Late"}}},
	}
	creator := &fakeCreator{inserted: true}
	n := NewOverdueNotifier(repo, creator)
	n.now = func() time.Time { return time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC) } // 12:00 UTC

	if generated, _ := n.Run(context.Background()); generated != 0 || len(creator.calls) != 0 {
		t.Fatalf("want 0/0 outside reminder hour, got %d/%d", generated, len(creator.calls))
	}
}

// Per-user isolation: one user's repo error must not abort the batch.
func TestOverdueRun_PerUserIsolation(t *testing.T) {
	repo := &fakeRepo{
		users: []RemindableUser{
			{UserID: "u1", Timezone: "UTC"},
			{UserID: "u2", Timezone: "UTC"},
		},
		overdueErr:  map[string]error{"u1": errors.New("db down for u1")},
		overdueByID: map[string][]DueTask{"u2": {{ID: "t2", Title: "Late"}}},
	}
	creator := &fakeCreator{inserted: true}
	n := NewOverdueNotifier(repo, creator)
	n.now = func() time.Time { return time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) }

	generated, err := n.Run(context.Background())
	if err != nil {
		t.Fatalf("Run must not return a per-user error: %v", err)
	}
	if generated != 1 || len(creator.calls) != 1 || creator.calls[0].UserID != "u2" {
		t.Fatalf("want u2's notification despite u1 failing, got generated=%d calls=%d", generated, len(creator.calls))
	}
}
