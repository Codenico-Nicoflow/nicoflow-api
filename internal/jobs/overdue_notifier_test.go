package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

// AC1: overdue detected at local morning → one task_overdue per task.
func TestOverdueRun_FiresAtLocalMorning(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC"}},
		overdueByID: map[string][]DueTask{"u1": {{ID: "t1", Title: "Late report"}}},
	}
	creator := &fakeCreator{inserted: true}
	n := NewOverdueNotifier(repo, creator)
	n.now = func() time.Time { return time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) }

	generated, err := n.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if generated.Fired != 1 || len(creator.calls) != 1 {
		t.Fatalf("generated=%d calls=%d, want 1/1", generated.Fired, len(creator.calls))
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
	if generated, _ := n.Run(context.Background(), false); generated.Fired != 1 {
		t.Fatalf("generated = %d, want 1 (free plan must still fire)", generated.Fired)
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

	generated, err := n.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if generated.Fired != 0 || len(creator.calls) != 1 {
		t.Fatalf("generated=%d calls=%d, want 0/1 (duplicate skipped, attempt made)", generated.Fired, len(creator.calls))
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

	if generated, _ := n.Run(context.Background(), false); generated.Fired != 0 || len(creator.calls) != 0 {
		t.Fatalf("want 0/0 outside reminder hour, got %d/%d", generated.Fired, len(creator.calls))
	}
}

// AC2 (NIC-1591): a user who disabled the overdue family gets no notification,
// even with an overdue task at their reminder hour.
func TestOverdueRun_RespectsFamilyToggle(t *testing.T) {
	repo := &fakeRepo{
		familiesOff: true, // overdue_enabled = false for u1
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC"}},
		overdueByID: map[string][]DueTask{"u1": {{ID: "t1", Title: "Late"}}},
	}
	creator := &fakeCreator{inserted: true}
	n := NewOverdueNotifier(repo, creator)
	n.now = func() time.Time { return time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) }

	if generated, _ := n.Run(context.Background(), false); generated.Fired != 0 || len(creator.calls) != 0 {
		t.Fatalf("want 0/0 when overdue disabled, got %d/%d", generated.Fired, len(creator.calls))
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

	generated, err := n.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run must not return a per-user error: %v", err)
	}
	if generated.Fired != 1 || len(creator.calls) != 1 || creator.calls[0].UserID != "u2" {
		t.Fatalf("want u2's notification despite u1 failing, got generated=%d calls=%d", generated.Fired, len(creator.calls))
	}
}

func TestOverdueRun_MetadataContainsTaskAndProject(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC"}},
		overdueByID: map[string][]DueTask{"u1": {{ID: "t1", Title: "Late report", ProjectID: "p1"}}},
	}
	creator := &fakeCreator{inserted: true}
	n := NewOverdueNotifier(repo, creator)
	n.now = func() time.Time { return time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) }

	if _, err := n.Run(context.Background(), false); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(creator.calls) != 1 {
		t.Fatalf("Create calls = %d, want 1", len(creator.calls))
	}

	var meta struct {
		EntityType string `json:"entityType"`
		EntityID   string `json:"entityId"`
		ProjectID  string `json:"projectId"`
	}
	if err := json.Unmarshal(creator.calls[0].Metadata, &meta); err != nil {
		t.Fatalf("Metadata unmarshal: %v", err)
	}
	if meta.EntityType != "task" || meta.EntityID != "t1" || meta.ProjectID != "p1" {
		t.Fatalf("metadata = %+v, want {task, t1, p1}", meta)
	}
}
