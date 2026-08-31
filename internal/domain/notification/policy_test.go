package notification

import "testing"

func TestIsProType(t *testing.T) {
	tests := []struct {
		typ   string
		isPro bool
	}{
		// Free types.
		{TypeTaskCompleted, false},
		{TypeProjectCompleted, false},
		{TypeSystemAnnouncement, false},
		{TypeMorningDigest, false},
		{TypeEveningDigest, false},
		// Pro types.
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
		{"task completed", DedupeTaskCompleted("tsk1"), "task_completed:tsk1"},
		{"project completed", DedupeProjectCompleted("prj1", "2026-07-15T08:00:00Z"), "project_completed:prj1:2026-07-15T08:00:00Z"},
		{"inbox zero", DedupeInboxZero("u1", "2026-07-15"), "inbox_zero:u1:2026-07-15"},
		{"streak milestone", DedupeStreakMilestone("u1", 7), "streak_milestone:u1:7"},
		{"morning digest", DedupeMorningDigest("u1", "2026-07-15"), "morning_digest:u1:2026-07-15"},
		{"evening digest", DedupeEveningDigest("u1", "2026-07-15"), "evening_digest:u1:2026-07-15"},
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
	a := DedupeMorningDigest("u1", "2026-07-15")
	b := DedupeMorningDigest("u1", "2026-07-15")
	if *a != *b {
		t.Errorf("same inputs produced different keys: %q vs %q", *a, *b)
	}
}
