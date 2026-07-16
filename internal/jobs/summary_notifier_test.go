package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

func TestSummaryLocalToday(t *testing.T) {
	// 2026-07-14 17:00 UTC = 20:00 Asia/Jerusalem (UTC+3 summer) → end-of-day hour.
	at17UTC := time.Date(2026, 7, 14, 17, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		now      time.Time
		tz       string
		wantDate string
		wantOK   bool
	}{
		{"local 20:00 → today", at17UTC, "Asia/Jerusalem", "2026-07-14", true},
		{"UTC 20:00 → today", time.Date(2026, 7, 14, 20, 0, 0, 0, time.UTC), "UTC", "2026-07-14", true},
		{"local 08:00 → skip", time.Date(2026, 7, 14, 5, 0, 0, 0, time.UTC), "Asia/Jerusalem", "", false},
		{"bad timezone → skip", at17UTC, "Not/AZone", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := summaryLocalToday(tt.now, tt.tz)
			if ok != tt.wantOK || got != tt.wantDate {
				t.Fatalf("got (%q,%v), want (%q,%v)", got, ok, tt.wantDate, tt.wantOK)
			}
		})
	}
}

func TestCurrentStreak(t *testing.T) {
	tests := []struct {
		name  string
		dates []string
		today string
		want  int
	}{
		{"no dates", nil, "2026-07-14", 0},
		{"today only", []string{"2026-07-14"}, "2026-07-14", 1},
		{"3 consecutive", []string{"2026-07-14", "2026-07-13", "2026-07-12"}, "2026-07-14", 3},
		{"gap breaks", []string{"2026-07-14", "2026-07-13", "2026-07-11"}, "2026-07-14", 2},
		{"latest not today → 0", []string{"2026-07-13", "2026-07-12"}, "2026-07-14", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := currentStreak(tt.dates, tt.today); got != tt.want {
				t.Fatalf("currentStreak = %d, want %d", got, tt.want)
			}
		})
	}
}

// consecutiveDates builds a descending run of `n` ISO dates ending at `end`.
func consecutiveDates(end string, n int) []string {
	day, _ := time.Parse(scheduledForLayout, end)
	out := make([]string, 0, n)
	for i := range n {
		out = append(out, day.AddDate(0, 0, -i).Format(scheduledForLayout))
	}
	return out
}

func at2000UTC() time.Time { return time.Date(2026, 7, 14, 20, 0, 0, 0, time.UTC) }

// AC1: Pro user completed N>0 today → one daily_summary with count N.
func TestSummaryRun_DailySummary(t *testing.T) {
	repo := &fakeRepo{
		users:     []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: planPro}},
		completed: map[string]int{"u1": 4},
	}
	creator := &fakeCreator{inserted: true}
	n := NewSummaryNotifier(repo, creator)
	n.now = at2000UTC

	generated, err := n.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if generated != 1 || len(creator.calls) != 1 {
		t.Fatalf("generated=%d calls=%d, want 1/1", generated, len(creator.calls))
	}
	got := creator.calls[0]
	if got.Type != notification.TypeDailySummary ||
		got.DedupeKey == nil || *got.DedupeKey != "daily_summary:u1:2026-07-14" {
		t.Fatalf("notification = %+v, want daily_summary + dedupe daily_summary:u1:2026-07-14", got)
	}
	var meta struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(got.Metadata, &meta); err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if meta.Count != 4 {
		t.Fatalf("count = %d, want 4", meta.Count)
	}
}

// No completions today → no summary, no streak.
func TestSummaryRun_NoCompletionsSilent(t *testing.T) {
	repo := &fakeRepo{
		users:     []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: planPro}},
		completed: map[string]int{"u1": 0},
	}
	creator := &fakeCreator{inserted: true}
	n := NewSummaryNotifier(repo, creator)
	n.now = at2000UTC

	if generated, _ := n.Run(context.Background()); generated != 0 || len(creator.calls) != 0 {
		t.Fatalf("no completions → silent, got generated=%d calls=%d", generated, len(creator.calls))
	}
}

// Free user is skipped (both types Pro).
func TestSummaryRun_FreeUserSkipped(t *testing.T) {
	repo := &fakeRepo{
		users:     []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: "free"}},
		completed: map[string]int{"u1": 5},
	}
	creator := &fakeCreator{inserted: true}
	n := NewSummaryNotifier(repo, creator)
	n.now = at2000UTC

	if generated, _ := n.Run(context.Background()); generated != 0 || len(creator.calls) != 0 {
		t.Fatalf("free user must be skipped, got generated=%d calls=%d", generated, len(creator.calls))
	}
}

// AC2: streak crossing a milestone (7) → summary + streak_milestone, deduped per milestone.
func TestSummaryRun_StreakMilestone(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: planPro}},
		completed:   map[string]int{"u1": 1},
		streakDates: map[string][]string{"u1": consecutiveDates("2026-07-14", 7)},
	}
	creator := &fakeCreator{inserted: true}
	n := NewSummaryNotifier(repo, creator)
	n.now = at2000UTC

	generated, err := n.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if generated != 2 || len(creator.calls) != 2 {
		t.Fatalf("want summary + streak, got generated=%d calls=%d", generated, len(creator.calls))
	}
	streak := creator.calls[1]
	if streak.Type != notification.TypeStreakMilestone ||
		streak.DedupeKey == nil || *streak.DedupeKey != "streak_milestone:u1:7" {
		t.Fatalf("streak notification = %+v, want streak_milestone + dedupe streak_milestone:u1:7", streak)
	}
}

// AC2: a non-milestone streak (5 days) → summary only, no streak notification.
func TestSummaryRun_NonMilestoneStreakNoFire(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: planPro}},
		completed:   map[string]int{"u1": 1},
		streakDates: map[string][]string{"u1": consecutiveDates("2026-07-14", 5)},
	}
	creator := &fakeCreator{inserted: true}
	n := NewSummaryNotifier(repo, creator)
	n.now = at2000UTC

	if _, err := n.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(creator.calls) != 1 || creator.calls[0].Type != notification.TypeDailySummary {
		t.Fatalf("want only daily_summary at streak 5, got %+v", creator.calls)
	}
}

// AC3: idempotent — dedupe-held rows attempted but not counted.
func TestSummaryRun_Idempotent(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: planPro}},
		completed:   map[string]int{"u1": 3},
		streakDates: map[string][]string{"u1": consecutiveDates("2026-07-14", 30)},
	}
	creator := &fakeCreator{inserted: false} // dedupe held
	n := NewSummaryNotifier(repo, creator)
	n.now = at2000UTC

	if generated, _ := n.Run(context.Background()); generated != 0 || len(creator.calls) != 2 {
		t.Fatalf("want 0 generated + 2 attempts, got generated=%d calls=%d", generated, len(creator.calls))
	}
}

func TestSummaryRun_SkipsUsersNotAtHour(t *testing.T) {
	repo := &fakeRepo{
		users:     []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: planPro}},
		completed: map[string]int{"u1": 5},
	}
	creator := &fakeCreator{inserted: true}
	n := NewSummaryNotifier(repo, creator)
	n.now = func() time.Time { return time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) } // 08:00

	if generated, _ := n.Run(context.Background()); generated != 0 || len(creator.calls) != 0 {
		t.Fatalf("want 0/0 outside end-of-day hour, got %d/%d", generated, len(creator.calls))
	}
}

// Per-user isolation: one user's count error must not abort the batch.
func TestSummaryRun_PerUserIsolation(t *testing.T) {
	repo := &fakeRepo{
		users: []RemindableUser{
			{UserID: "u1", Timezone: "UTC", Plan: planPro},
			{UserID: "u2", Timezone: "UTC", Plan: planPro},
		},
		completeErr: map[string]error{"u1": errors.New("db down for u1")},
		completed:   map[string]int{"u2": 2},
	}
	creator := &fakeCreator{inserted: true}
	n := NewSummaryNotifier(repo, creator)
	n.now = at2000UTC

	generated, err := n.Run(context.Background())
	if err != nil {
		t.Fatalf("Run must not return a per-user error: %v", err)
	}
	if generated != 1 || len(creator.calls) != 1 || creator.calls[0].UserID != "u2" {
		t.Fatalf("want u2's summary despite u1 failing, got generated=%d calls=%d", generated, len(creator.calls))
	}
}
