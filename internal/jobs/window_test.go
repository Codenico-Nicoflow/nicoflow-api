package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
)

// TestInFireWindow drives the window purely from table inputs — never asserting a
// constant against itself — so it actually pins the behaviour (DoD requirement).
func TestInFireWindow(t *testing.T) {
	tests := []struct {
		name         string
		hour         int
		reminderHour int
		want         bool
	}{
		// Morning window [8,11): fires at hour, hour+1, hour+2; not hour+3.
		{"fires at reminder hour", 8, 8, true},
		{"fires at hour+1 (catch-up)", 9, 8, true},
		{"fires at hour+2 (catch-up)", 10, 8, true},
		{"does NOT fire at hour+3", 11, 8, false},
		{"does NOT fire before the hour", 7, 8, false},
		{"does NOT fire well after", 15, 8, false},

		// Clamp: a 22:00 preference fires at 22 and 23, then stops. The unclamped
		// window (22+3=25) would leave hour<25 true all night — the mandatory clamp
		// caps it at 24.
		{"22:00 fires at 22", 22, 22, true},
		{"22:00 fires at 23 (one spare)", 23, 22, true},
		{"22:00 does NOT fire at 00 (clamp holds the night shut)", 0, 22, false},
		{"22:00 does NOT fire at 01 (clamp)", 1, 22, false},
		{"22:00 does NOT fire at 02 (clamp)", 2, 22, false},

		// A 21:00 preference: [21,24) → fires 21/22/23, not 00.
		{"21:00 fires at 23 (two spares)", 23, 21, true},
		{"21:00 does NOT fire at 00 (clamp)", 0, 21, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inFireWindow(tt.hour, tt.reminderHour); got != tt.want {
				t.Fatalf("inFireWindow(%d, %d) = %v, want %v", tt.hour, tt.reminderHour, got, tt.want)
			}
		})
	}
}

// TestSweep_WindowCatchUp drives a real sweep across the window: it fires at the
// reminder hour and the two catch-up ticks, and stays silent at hour+3. Driven
// from the same UTC clock so the window logic — not a duplicated constant — is
// what's under test.
func TestSweep_WindowCatchUp(t *testing.T) {
	tests := []struct {
		name     string
		utcHour  int
		wantFire bool
	}{
		{"fires at reminder hour (08:00)", 8, true},
		{"fires at hour+1 (09:00)", 9, true},
		{"fires at hour+2 (10:00)", 10, true},
		{"silent at hour+3 (11:00)", 11, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{
				users:       []RemindableUser{{UserID: "u1", Timezone: "UTC", MorningHour: 8}},
				overdueByID: map[string][]DueTask{"u1": {{ID: "t1", Title: "Late"}}},
			}
			creator := &fakeCreator{inserted: true}
			n := NewOverdueNotifier(repo, creator)
			n.now = func() time.Time { return time.Date(2026, 7, 14, tt.utcHour, 0, 0, 0, time.UTC) }

			b, err := n.Run(context.Background(), false)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			wantFired := 0
			if tt.wantFire {
				wantFired = 1
			}
			if b.Fired != wantFired {
				t.Fatalf("Fired = %d, want %d", b.Fired, wantFired)
			}
			if !tt.wantFire && b.Skipped[skipOutsideWindow] != 1 {
				t.Fatalf("outside_window skip = %d, want 1", b.Skipped[skipOutsideWindow])
			}
		})
	}
}

// TestSweep_EveningWindowClamp pins the mandatory clamp end-to-end for the summary
// sweep (the only evening-hour sweep): a 22:00 preference fires at 22 and 23 but
// NOT at 00:00 the next day. Without the clamp, 00:00 would fire and its
// local-date dedupe key would silently eat the next evening's real summary.
func TestSweep_EveningWindowClamp(t *testing.T) {
	tests := []struct {
		name     string
		utcHour  int
		wantFire bool
	}{
		{"fires at 22:00", 22, true},
		{"fires at 23:00 (spare)", 23, true},
		{"does NOT fire at 00:00 (clamp)", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &fakeRepo{
				users:     []RemindableUser{{UserID: "u1", Timezone: "UTC", Plan: planPro, EveningHour: 22}},
				completed: map[string]int{"u1": 3},
			}
			creator := &fakeCreator{inserted: true}
			n := NewSummaryNotifier(repo, creator)
			n.now = func() time.Time { return time.Date(2026, 7, 14, tt.utcHour, 0, 0, 0, time.UTC) }

			b, err := n.Run(context.Background(), false)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			fired := b.Fired > 0
			if fired != tt.wantFire {
				t.Fatalf("fired = %v (Fired=%d), want %v", fired, b.Fired, tt.wantFire)
			}
		})
	}
}

// TestSweep_DSTSpringForward: a zone whose 02:00 does not exist (America/New_York
// on 2026-03-08, 02:00→03:00) still fires. The sweep never calls into the missing
// wall time — it reads the actual local hour of a real UTC instant — so a
// morning_hour that lands in the gap simply resolves to the post-jump hour and
// the window still matches. Here 12:00 UTC = 08:00 EDT (post-jump), morning_hour 8.
func TestSweep_DSTSpringForward(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "America/New_York", MorningHour: 8}},
		overdueByID: map[string][]DueTask{"u1": {{ID: "t1", Title: "Late"}}},
	}
	creator := &fakeCreator{inserted: true}
	n := NewOverdueNotifier(repo, creator)
	// 2026-03-08 12:00 UTC: after the spring-forward, New York is UTC-4 (EDT) → 08:00.
	n.now = func() time.Time { return time.Date(2026, 3, 8, 12, 0, 0, 0, time.UTC) }

	b, err := n.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if b.Fired != 1 {
		t.Fatalf("Fired = %d, want 1 (DST spring-forward must still fire)", b.Fired)
	}
}

// TestSweep_BadTimezoneBreakdown: a bad zone is recorded as bad_timezone, never
// fires, and never crashes the sweep. This is the fault that hid the original bug.
func TestSweep_BadTimezoneBreakdown(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "Mars/Olympus", MorningHour: 8}},
		overdueByID: map[string][]DueTask{"u1": {{ID: "t1", Title: "Late"}}},
	}
	creator := &fakeCreator{inserted: true}
	n := NewOverdueNotifier(repo, creator)
	n.now = func() time.Time { return time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) }

	b, err := n.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if b.Considered != 1 || b.Fired != 0 || b.Skipped[skipBadTimezone] != 1 {
		t.Fatalf("breakdown = %+v, want considered 1 / fired 0 / bad_timezone 1", b)
	}
	if len(creator.calls) != 0 {
		t.Fatalf("bad timezone must not create, got %d calls", len(creator.calls))
	}
}

// TestSweep_DedupeSuppressesCatchUp: across the three window ticks, dedupe (Create
// reporting inserted=false) suppresses the 2nd and 3rd fires. Each tick attempts a
// Create but only the first is counted — exactly what keeps a catch-up from
// double-notifying.
func TestSweep_DedupeSuppressesCatchUp(t *testing.T) {
	// Simulate three hourly ticks against a creator that inserts once then dedupes.
	creator := &dedupeOnceCreator{}
	fired := 0
	for _, utcHour := range []int{8, 9, 10} {
		repo := &fakeRepo{
			users:       []RemindableUser{{UserID: "u1", Timezone: "UTC", MorningHour: 8}},
			overdueByID: map[string][]DueTask{"u1": {{ID: "t1", Title: "Late"}}},
		}
		n := NewOverdueNotifier(repo, creator)
		n.now = func() time.Time { return time.Date(2026, 7, 14, utcHour, 0, 0, 0, time.UTC) }
		b, err := n.Run(context.Background(), false)
		if err != nil {
			t.Fatalf("Run at %02d:00: %v", utcHour, err)
		}
		fired += b.Fired
	}
	if fired != 1 {
		t.Fatalf("total fired across window = %d, want 1 (dedupe suppresses catch-up ticks)", fired)
	}
	if creator.calls != 3 {
		t.Fatalf("Create attempts = %d, want 3 (one per tick)", creator.calls)
	}
}

// TestSweep_DryRunInsertsNothing: dryRun computes the breakdown (a user is in
// window and would fire) but performs no Create.
func TestSweep_DryRunInsertsNothing(t *testing.T) {
	repo := &fakeRepo{
		users:       []RemindableUser{{UserID: "u1", Timezone: "UTC", MorningHour: 8}},
		overdueByID: map[string][]DueTask{"u1": {{ID: "t1", Title: "Late"}}},
	}
	creator := &fakeCreator{inserted: true}
	n := NewOverdueNotifier(repo, creator)
	n.now = func() time.Time { return time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC) }

	b, err := n.Run(context.Background(), true) // dryRun
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if b.Considered != 1 {
		t.Fatalf("considered = %d, want 1", b.Considered)
	}
	if len(creator.calls) != 0 {
		t.Fatalf("dryRun must not create, got %d calls", len(creator.calls))
	}
}

// dedupeOnceCreator reports inserted=true on the first Create and false thereafter,
// mimicking the notifications (user_id, dedupe_key) unique index.
type dedupeOnceCreator struct {
	calls int
}

func (c *dedupeOnceCreator) Create(_ context.Context, _ notification.Notification) (notification.NotificationView, bool, error) {
	c.calls++
	return notification.NotificationView{}, c.calls == 1, nil
}
