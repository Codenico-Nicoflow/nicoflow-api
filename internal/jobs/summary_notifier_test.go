package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

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

func eveningUser(userID, plan string) RemindableUser {
	return RemindableUser{UserID: userID, Timezone: "UTC", Plan: plan, EveningDigestEnabled: true, EveningHour: 20}
}

// completed>0 → one evening_digest with completed+remaining counts (all plans).
func TestSummaryRun_DigestFiresOnCompletion(t *testing.T) {
	repo := &fakeRepo{
		users:     []RemindableUser{eveningUser("u1", "free")},
		completed: map[string]int{"u1": 4},
		openTasks: map[string]int{"u1": 2},
	}
	creator := &fakeCreator{inserted: true}
	n := NewSummaryNotifier(repo, creator)
	n.now = at2000UTC

	generated, err := n.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if generated.Fired != 1 || len(creator.calls) != 1 {
		t.Fatalf("generated=%d calls=%d, want 1/1", generated.Fired, len(creator.calls))
	}
	got := creator.calls[0]
	if got.Type != notification.TypeEveningDigest ||
		got.DedupeKey == nil || *got.DedupeKey != "evening_digest:u1:2026-07-14" {
		t.Fatalf("notification = %+v, want evening_digest + dedupe evening_digest:u1:2026-07-14", got)
	}
	var meta struct {
		Completed int `json:"completed"`
		Remaining int `json:"remaining"`
	}
	if err := json.Unmarshal(got.Metadata, &meta); err != nil {
		t.Fatalf("metadata: %v", err)
	}
	if meta.Completed != 4 || meta.Remaining != 2 {
		t.Fatalf("meta = %+v, want completed=4 remaining=2", meta)
	}
}

// Zero completions but tasks remain → still fires ("0 done today, N left").
func TestSummaryRun_FiresOnZeroCompletionsWithRemaining(t *testing.T) {
	repo := &fakeRepo{
		users:     []RemindableUser{eveningUser("u1", "free")},
		completed: map[string]int{"u1": 0},
		openTasks: map[string]int{"u1": 3},
	}
	creator := &fakeCreator{inserted: true}
	n := NewSummaryNotifier(repo, creator)
	n.now = at2000UTC

	generated, _ := n.Run(context.Background(), false)
	if generated.Fired != 1 || len(creator.calls) != 1 {
		t.Fatalf("generated=%d calls=%d, want 1/1 (0 completed, work remains)", generated.Fired, len(creator.calls))
	}
}

// True-empty state: zero completed AND zero remaining → silent.
func TestSummaryRun_SilentOnTrueEmpty(t *testing.T) {
	repo := &fakeRepo{
		users:     []RemindableUser{eveningUser("u1", "free")},
		completed: map[string]int{"u1": 0},
		openTasks: map[string]int{"u1": 0},
	}
	creator := &fakeCreator{inserted: true}
	n := NewSummaryNotifier(repo, creator)
	n.now = at2000UTC

	if generated, _ := n.Run(context.Background(), false); generated.Fired != 0 || len(creator.calls) != 0 {
		t.Fatalf("true-empty must stay silent, got generated=%d calls=%d", generated.Fired, len(creator.calls))
	}
}

// A user with the evening digest toggled off (and no active streak) gets nothing.
func TestSummaryRun_ToggleOffSkips(t *testing.T) {
	u := eveningUser("u1", "free")
	u.EveningDigestEnabled = false
	repo := &fakeRepo{
		users:     []RemindableUser{u},
		completed: map[string]int{"u1": 5},
	}
	creator := &fakeCreator{inserted: true}
	n := NewSummaryNotifier(repo, creator)
	n.now = at2000UTC

	if generated, _ := n.Run(context.Background(), false); generated.Fired != 0 || len(creator.calls) != 0 {
		t.Fatalf("toggled-off user must get nothing, got generated=%d calls=%d", generated.Fired, len(creator.calls))
	}
}

// AC2: streak crossing a milestone (7) → digest + streak_milestone, Pro only.
func TestSummaryRun_StreakMilestone(t *testing.T) {
	u := eveningUser("u1", "pro")
	u.StreaksEnabled = true
	repo := &fakeRepo{
		users:       []RemindableUser{u},
		completed:   map[string]int{"u1": 1},
		streakDates: map[string][]string{"u1": consecutiveDates("2026-07-14", 7)},
	}
	creator := &fakeCreator{inserted: true}
	n := NewSummaryNotifier(repo, creator)
	n.now = at2000UTC

	generated, err := n.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if generated.Fired != 2 || len(creator.calls) != 2 {
		t.Fatalf("want digest + streak, got generated=%d calls=%d", generated.Fired, len(creator.calls))
	}
	streak := creator.calls[1]
	if streak.Type != notification.TypeStreakMilestone ||
		streak.DedupeKey == nil || *streak.DedupeKey != "streak_milestone:u1:7" {
		t.Fatalf("streak notification = %+v, want streak_milestone + dedupe streak_milestone:u1:7", streak)
	}
}

// Free user never gets a streak, even with a milestone streak — Pro-only perk.
func TestSummaryRun_FreeUserNoStreak(t *testing.T) {
	u := eveningUser("u1", "free")
	u.StreaksEnabled = true
	repo := &fakeRepo{
		users:       []RemindableUser{u},
		completed:   map[string]int{"u1": 1},
		streakDates: map[string][]string{"u1": consecutiveDates("2026-07-14", 7)},
	}
	creator := &fakeCreator{inserted: true}
	n := NewSummaryNotifier(repo, creator)
	n.now = at2000UTC

	generated, _ := n.Run(context.Background(), false)
	if generated.Fired != 1 || len(creator.calls) != 1 || creator.calls[0].Type != notification.TypeEveningDigest {
		t.Fatalf("free user must get digest only, got generated=%d calls=%+v", generated.Fired, creator.calls)
	}
}

// A non-milestone streak (5 days) → digest only, no streak notification.
func TestSummaryRun_NonMilestoneStreakNoFire(t *testing.T) {
	u := eveningUser("u1", "pro")
	u.StreaksEnabled = true
	repo := &fakeRepo{
		users:       []RemindableUser{u},
		completed:   map[string]int{"u1": 1},
		streakDates: map[string][]string{"u1": consecutiveDates("2026-07-14", 5)},
	}
	creator := &fakeCreator{inserted: true}
	n := NewSummaryNotifier(repo, creator)
	n.now = at2000UTC

	if _, err := n.Run(context.Background(), false); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(creator.calls) != 1 || creator.calls[0].Type != notification.TypeEveningDigest {
		t.Fatalf("want only evening_digest at streak 5, got %+v", creator.calls)
	}
}

// AC3: idempotent — dedupe-held rows attempted but not counted.
func TestSummaryRun_Idempotent(t *testing.T) {
	u := eveningUser("u1", "pro")
	u.StreaksEnabled = true
	repo := &fakeRepo{
		users:       []RemindableUser{u},
		completed:   map[string]int{"u1": 3},
		streakDates: map[string][]string{"u1": consecutiveDates("2026-07-14", 30)},
	}
	creator := &fakeCreator{inserted: false} // dedupe held
	n := NewSummaryNotifier(repo, creator)
	n.now = at2000UTC

	if generated, _ := n.Run(context.Background(), false); generated.Fired != 0 || len(creator.calls) != 2 {
		t.Fatalf("want 0 generated + 2 attempts, got generated=%d calls=%d", generated.Fired, len(creator.calls))
	}
}

func TestSummaryRun_SkipsUsersNotAtHour(t *testing.T) {
	repo := &fakeRepo{
		users:     []RemindableUser{eveningUser("u1", "free")},
		completed: map[string]int{"u1": 5},
	}
	creator := &fakeCreator{inserted: true}
	n := NewSummaryNotifier(repo, creator)
	n.now = func() time.Time { return time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) } // 08:00

	if generated, _ := n.Run(context.Background(), false); generated.Fired != 0 || len(creator.calls) != 0 {
		t.Fatalf("want 0/0 outside end-of-day hour, got %d/%d", generated.Fired, len(creator.calls))
	}
}

// Per-user isolation: one user's count error must not abort the batch.
func TestSummaryRun_PerUserIsolation(t *testing.T) {
	repo := &fakeRepo{
		users:        []RemindableUser{eveningUser("u1", "free"), eveningUser("u2", "free")},
		completedErr: map[string]error{"u1": errors.New("db down for u1")},
		completed:    map[string]int{"u2": 2},
	}
	creator := &fakeCreator{inserted: true}
	n := NewSummaryNotifier(repo, creator)
	n.now = at2000UTC

	generated, err := n.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run must not return a per-user error: %v", err)
	}
	if generated.Fired != 1 || len(creator.calls) != 1 || creator.calls[0].UserID != "u2" {
		t.Fatalf("want u2's digest despite u1 failing, got generated=%d calls=%d", generated.Fired, len(creator.calls))
	}
}
