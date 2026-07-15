package notification

import "testing"

func TestIsProType(t *testing.T) {
	tests := []struct {
		typ   string
		isPro bool
	}{
		// Free types.
		{TypeTaskDueSoon, false},
		{TypeTaskOverdue, false},
		{TypeTaskScheduledToday, false},
		{TypeTaskCompleted, false},
		{TypeProjectCompleted, false},
		{TypeSystemAnnouncement, false},
		// Pro types.
		{TypeNothingScheduled, true},
		{TypeInboxUnprocessed, true},
		{TypeInboxStale, true},
		{TypeDailySummary, true},
		{TypeInboxZero, true},
		{TypeStreakMilestone, true},
		// Unknown → treated as free (not Pro).
		{"something_else", false},
	}
	for _, tt := range tests {
		if got := IsProType(tt.typ); got != tt.isPro {
			t.Errorf("IsProType(%q) = %v, want %v", tt.typ, got, tt.isPro)
		}
	}
}

func TestDedupeBuilders(t *testing.T) {
	tests := []struct {
		name string
		got  *string
		want string
	}{
		{"overdue", DedupeTaskOverdue("tsk1", "2026-07-15"), "task_overdue:tsk1:2026-07-15"},
		{"scheduled today", DedupeTaskScheduledToday("u1", "2026-07-15"), "task_scheduled_today:u1:2026-07-15"},
		{"day plan nudge", DedupeDayPlanNudge("u1", "2026-07-15"), "day_plan_nudge:u1:2026-07-15"},
		{"inbox unprocessed", DedupeInboxUnprocessed("u1", "2026-07-15"), "inbox_unprocessed:u1:2026-07-15"},
		{"inbox stale", DedupeInboxStale("u1", "2026-W29"), "inbox_stale:u1:2026-W29"},
		{"task completed", DedupeTaskCompleted("tsk1"), "task_completed:tsk1"},
		{"project completed", DedupeProjectCompleted("prj1", "2026-07-15"), "project_completed:prj1:2026-07-15"},
		{"daily summary", DedupeDailySummary("u1", "2026-07-15"), "daily_summary:u1:2026-07-15"},
		{"inbox zero", DedupeInboxZero("u1", "2026-07-15"), "inbox_zero:u1:2026-07-15"},
		{"streak milestone", DedupeStreakMilestone("u1", 7), "streak_milestone:u1:7"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got == nil {
				t.Fatal("builder returned nil")
			}
			if *tt.got != tt.want {
				t.Errorf("key = %q, want %q", *tt.got, tt.want)
			}
		})
	}
}

// TestDedupeStable: the same logical event within its window yields the same key,
// so ON CONFLICT DO NOTHING suppresses the duplicate.
func TestDedupeStable(t *testing.T) {
	a := DedupeTaskOverdue("tsk1", "2026-07-15")
	b := DedupeTaskOverdue("tsk1", "2026-07-15")
	if *a != *b {
		t.Errorf("same inputs produced different keys: %q vs %q", *a, *b)
	}
}
