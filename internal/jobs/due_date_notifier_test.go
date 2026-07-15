package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

func TestReminderTargetDate(t *testing.T) {
	// 2026-07-14 05:00 UTC = 08:00 in Asia/Jerusalem (UTC+3, summer).
	at05UTC := time.Date(2026, 7, 14, 5, 0, 0, 0, time.UTC)
	// 2026-07-14 06:00 UTC = 09:00 in Asia/Jerusalem → not the reminder hour.
	at06UTC := time.Date(2026, 7, 14, 6, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		now        time.Time
		tz         string
		beforeMins int
		wantDate   string
		wantOK     bool
	}{
		{"local 08:00, 24h lead → local tomorrow", at05UTC, "Asia/Jerusalem", 1440, "2026-07-15", true},
		{"local 08:00, 0 lead → local today", at05UTC, "Asia/Jerusalem", 0, "2026-07-14", true},
		{"local 08:00, 48h lead → +2 days", at05UTC, "Asia/Jerusalem", 2880, "2026-07-16", true},
		{"local 09:00 → skip", at06UTC, "Asia/Jerusalem", 1440, "", false},
		{"UTC user at 08:00 fires", time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC), "UTC", 1440, "2026-07-15", true},
		{"UTC user at 05:00 skips", at05UTC, "UTC", 1440, "", false},
		{"bad timezone → skip", at05UTC, "Not/AZone", 1440, "", false},
		{"negative lead clamps to today", at05UTC, "Asia/Jerusalem", -100, "2026-07-14", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := reminderTargetDate(tt.now, tt.tz, tt.beforeMins)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got != tt.wantDate {
				t.Fatalf("date = %q, want %q", got, tt.wantDate)
			}
		})
	}
}

func TestDedupeKey(t *testing.T) {
	got := dedupeKey("task-42", "2026-07-15")
	want := "task_due_soon:task-42:2026-07-15"
	if got == nil || *got != want {
		t.Fatalf("dedupeKey = %v, want %q", got, want)
	}
}

// ── fakes ────────────────────────────────────────────────────────────────────

type fakeRepo struct {
	users       []RemindableUser
	tasksByID   map[string][]DueTask // keyed by userID (scheduled-on lookups)
	tasksErr    map[string]error     // per-user scheduled-on error, for isolation tests
	hasWorkByID map[string]bool      // keyed by userID (HasActiveWork result)
	hasWorkErr  map[string]error     // per-user HasActiveWork error
	usersErr    error
}

func (f *fakeRepo) ListRemindableUsers(_ context.Context) ([]RemindableUser, error) {
	return f.users, f.usersErr
}
func (f *fakeRepo) ListTasksScheduledOn(_ context.Context, userID, _ string) ([]DueTask, error) {
	if err := f.tasksErr[userID]; err != nil {
		return nil, err
	}
	return f.tasksByID[userID], nil
}
func (f *fakeRepo) HasActiveWork(_ context.Context, userID string) (bool, error) {
	if err := f.hasWorkErr[userID]; err != nil {
		return false, err
	}
	return f.hasWorkByID[userID], nil
}

type fakeCreator struct {
	calls    []notification.Notification
	inserted bool // what Create reports for "was a row inserted"
}

func (f *fakeCreator) Create(_ context.Context, n notification.Notification) (notification.NotificationView, bool, error) {
	f.calls = append(f.calls, n)
	return notification.NotificationView{}, f.inserted, nil
}

func TestRun_FiresForLocal0800User(t *testing.T) {
	repo := &fakeRepo{
		users:     []RemindableUser{{UserID: "u1", Timezone: "UTC", BeforeDueMinutes: 1440}},
		tasksByID: map[string][]DueTask{"u1": {{ID: "t1", Title: "Ship it"}}},
	}
	creator := &fakeCreator{inserted: true}
	n := NewDueDateNotifier(repo, creator, "")
	n.now = func() time.Time { return time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) }

	generated, err := n.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if generated != 1 {
		t.Fatalf("generated = %d, want 1", generated)
	}
	if len(creator.calls) != 1 {
		t.Fatalf("Create calls = %d, want 1", len(creator.calls))
	}
	got := creator.calls[0]
	if got.Type != notification.TypeTaskDueSoon || got.DedupeKey == nil ||
		*got.DedupeKey != "task_due_soon:t1:2026-07-15" {
		t.Fatalf("notification = %+v, want type task_due_soon + dedupe task_due_soon:t1:2026-07-15", got)
	}
}

func TestRun_SkipsUserNotAt0800(t *testing.T) {
	repo := &fakeRepo{
		users:     []RemindableUser{{UserID: "u1", Timezone: "UTC", BeforeDueMinutes: 1440}},
		tasksByID: map[string][]DueTask{"u1": {{ID: "t1", Title: "Ship it"}}},
	}
	creator := &fakeCreator{inserted: true}
	n := NewDueDateNotifier(repo, creator, "")
	n.now = func() time.Time { return time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC) } // 12:00 UTC

	generated, err := n.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if generated != 0 || len(creator.calls) != 0 {
		t.Fatalf("generated = %d, calls = %d, want 0/0 (not the reminder hour)", generated, len(creator.calls))
	}
}

func TestRun_DuplicateNotCounted(t *testing.T) {
	repo := &fakeRepo{
		users:     []RemindableUser{{UserID: "u1", Timezone: "UTC", BeforeDueMinutes: 1440}},
		tasksByID: map[string][]DueTask{"u1": {{ID: "t1", Title: "Ship it"}}},
	}
	// Create reports inserted=false → dedupe held, so it must not be counted.
	creator := &fakeCreator{inserted: false}
	n := NewDueDateNotifier(repo, creator, "")
	n.now = func() time.Time { return time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) }

	generated, err := n.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if generated != 0 {
		t.Fatalf("generated = %d, want 0 (duplicate skipped)", generated)
	}
	if len(creator.calls) != 1 {
		t.Fatalf("Create still attempted once, got %d calls", len(creator.calls))
	}
}
