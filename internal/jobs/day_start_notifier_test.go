package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

func TestDayStartLocalToday(t *testing.T) {
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
			got, ok := dayStartLocalToday(tt.now, tt.tz)
			if ok != tt.wantOK || got != tt.wantDate {
				t.Fatalf("got (%q,%v), want (%q,%v)", got, ok, tt.wantDate, tt.wantOK)
			}
		})
	}
}

// at0800UTC is a fixed clock at 08:00 UTC — the reminder hour for a UTC user.
func at0800UTC() time.Time { return time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) }

// AC1: N>0 tasks scheduled today → one task_scheduled_today with count metadata N
// (all plans). Free user still fires (FREE type).
func TestDayStartRun_ScheduledSummary(t *testing.T) {
	tests := []struct {
		name string
		plan string
	}{
		{"free plan fires", "free"},
		{"pro plan fires", planPro},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{
				users:     []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: tt.plan}},
				tasksByID: map[string][]DueTask{"u1": {{ID: "t1", Title: "A"}, {ID: "t2", Title: "B"}}},
			}
			creator := &fakeCreator{inserted: true}
			n := NewDayStartNotifier(repo, creator)
			n.now = at0800UTC

			generated, err := n.Run(context.Background())
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if generated != 1 || len(creator.calls) != 1 {
				t.Fatalf("generated=%d calls=%d, want 1/1", generated, len(creator.calls))
			}
			got := creator.calls[0]
			if got.Type != notification.TypeTaskScheduledToday {
				t.Fatalf("type = %q, want task_scheduled_today", got.Type)
			}
			if got.DedupeKey == nil || *got.DedupeKey != "task_scheduled_today:u1:2026-07-14" {
				t.Fatalf("dedupe = %v, want task_scheduled_today:u1:2026-07-14", got.DedupeKey)
			}
			var meta struct {
				Count int `json:"count"`
			}
			if err := json.Unmarshal(got.Metadata, &meta); err != nil {
				t.Fatalf("metadata unmarshal: %v", err)
			}
			if meta.Count != 2 {
				t.Fatalf("metadata count = %d, want 2", meta.Count)
			}
		})
	}
}

// A user with tasks scheduled gets the summary, never the nudge.
func TestDayStartRun_ScheduledUserGetsNoNudge(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: planPro}},
		tasksByID:   map[string][]DueTask{"u1": {{ID: "t1", Title: "A"}}},
		hasWorkByID: map[string]bool{"u1": true},
	}
	creator := &fakeCreator{inserted: true}
	n := NewDayStartNotifier(repo, creator)
	n.now = at0800UTC

	if _, err := n.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(creator.calls) != 1 || creator.calls[0].Type != notification.TypeTaskScheduledToday {
		t.Fatalf("want only a scheduled-today summary, got %+v", creator.calls)
	}
}

// AC2: Pro user, zero scheduled, has open work → one day_plan_nudge.
func TestDayStartRun_ProNudge(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: planPro}},
		tasksByID:   map[string][]DueTask{}, // nothing scheduled today
		hasWorkByID: map[string]bool{"u1": true},
	}
	creator := &fakeCreator{inserted: true}
	n := NewDayStartNotifier(repo, creator)
	n.now = at0800UTC

	generated, err := n.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if generated != 1 || len(creator.calls) != 1 {
		t.Fatalf("generated=%d calls=%d, want 1/1", generated, len(creator.calls))
	}
	got := creator.calls[0]
	if got.Type != notification.TypeNothingScheduled ||
		got.DedupeKey == nil || *got.DedupeKey != "day_plan_nudge:u1:2026-07-14" {
		t.Fatalf("notification = %+v, want day_plan_nudge + dedupe day_plan_nudge:u1:2026-07-14", got)
	}
}

// AC2: a free user in the same state (zero scheduled, has work) gets nothing.
func TestDayStartRun_FreeUserNoNudge(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: "free"}},
		tasksByID:   map[string][]DueTask{},
		hasWorkByID: map[string]bool{"u1": true},
	}
	creator := &fakeCreator{inserted: true}
	n := NewDayStartNotifier(repo, creator)
	n.now = at0800UTC

	if generated, _ := n.Run(context.Background()); generated != 0 || len(creator.calls) != 0 {
		t.Fatalf("free user must get nothing, got generated=%d calls=%d", generated, len(creator.calls))
	}
}

// Pro user with zero scheduled AND no open work → no nudge (nothing to plan).
func TestDayStartRun_ProNoWorkNoNudge(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: planPro}},
		tasksByID:   map[string][]DueTask{},
		hasWorkByID: map[string]bool{"u1": false},
	}
	creator := &fakeCreator{inserted: true}
	n := NewDayStartNotifier(repo, creator)
	n.now = at0800UTC

	if generated, _ := n.Run(context.Background()); generated != 0 || len(creator.calls) != 0 {
		t.Fatalf("no work → no nudge, got generated=%d calls=%d", generated, len(creator.calls))
	}
}

// AC3: idempotent — dedupe-held rows are attempted but not counted (both outputs).
func TestDayStartRun_Idempotent(t *testing.T) {
	tests := []struct {
		name     string
		tasks    []DueTask
		hasWork  bool
		wantType string
	}{
		{"summary held", []DueTask{{ID: "t1", Title: "A"}}, false, notification.TypeTaskScheduledToday},
		{"nudge held", nil, true, notification.TypeNothingScheduled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{
				users:       []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: planPro}},
				tasksByID:   map[string][]DueTask{"u1": tt.tasks},
				hasWorkByID: map[string]bool{"u1": tt.hasWork},
			}
			creator := &fakeCreator{inserted: false} // dedupe held
			n := NewDayStartNotifier(repo, creator)
			n.now = at0800UTC

			generated, err := n.Run(context.Background())
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if generated != 0 || len(creator.calls) != 1 || creator.calls[0].Type != tt.wantType {
				t.Fatalf("want 0 generated + 1 %s attempt, got generated=%d calls=%+v", tt.wantType, generated, creator.calls)
			}
		})
	}
}

func TestDayStartRun_SkipsUsersNotAtReminderHour(t *testing.T) {
	repo := &fakeRepo{
		users:     []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: planPro}},
		tasksByID: map[string][]DueTask{"u1": {{ID: "t1", Title: "A"}}},
	}
	creator := &fakeCreator{inserted: true}
	n := NewDayStartNotifier(repo, creator)
	n.now = func() time.Time { return time.Date(2026, 7, 14, 12, 0, 0, 0, time.UTC) } // 12:00 UTC

	if generated, _ := n.Run(context.Background()); generated != 0 || len(creator.calls) != 0 {
		t.Fatalf("want 0/0 outside reminder hour, got %d/%d", generated, len(creator.calls))
	}
}

// Per-user isolation: one user's repo error must not abort the batch.
func TestDayStartRun_PerUserIsolation(t *testing.T) {
	repo := &fakeRepo{
		users: []RemindableUser{
			{UserID: "u1", Timezone: "UTC", Plan: planPro},
			{UserID: "u2", Timezone: "UTC", Plan: planPro},
		},
		tasksErr:  map[string]error{"u1": errors.New("db down for u1")},
		tasksByID: map[string][]DueTask{"u2": {{ID: "t2", Title: "A"}}},
	}
	creator := &fakeCreator{inserted: true}
	n := NewDayStartNotifier(repo, creator)
	n.now = at0800UTC

	generated, err := n.Run(context.Background())
	if err != nil {
		t.Fatalf("Run must not return a per-user error: %v", err)
	}
	if generated != 1 || len(creator.calls) != 1 || creator.calls[0].UserID != "u2" {
		t.Fatalf("want u2's summary despite u1 failing, got generated=%d calls=%d", generated, len(creator.calls))
	}
}
