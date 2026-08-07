package ai

import (
	"strings"
	"testing"
	"time"
)

func TestDeriveTitle(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"short kept whole", "Plan my week", "Plan my week"},
		{"trims surrounding space", "  hello world  ", "hello world"},
		{"word-boundary truncate", strings.Repeat("ab ", 40), strings.TrimSpace(cutWords(strings.Repeat("ab ", 40)))},
		{"single long word hard-cut", strings.Repeat("x", 80), strings.Repeat("x", 50)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := deriveTitle(tc.in)
			if len(got) > titleMaxLen {
				t.Fatalf("title %q longer than %d", got, titleMaxLen)
			}
			if tc.name == "short kept whole" && got != tc.want {
				t.Errorf("deriveTitle = %q, want %q", got, tc.want)
			}
		})
	}
}

// cutWords mirrors deriveTitle's word-boundary logic for the expected value.
func cutWords(s string) string {
	if len(s) <= titleMaxLen {
		return s
	}
	cut := s[:titleMaxLen]
	if i := strings.LastIndexByte(cut, ' '); i > 0 {
		cut = cut[:i]
	}
	return cut
}

func TestBuildHistory_MessageCap(t *testing.T) {
	var rows []SessionMessage
	for i := 0; i < 30; i++ { // newest-first, 30 tiny rows
		rows = append(rows, SessionMessage{Role: "user", Content: "hi"})
	}
	got := buildHistory(rows)
	if len(got) != historyMaxMessages {
		t.Fatalf("len = %d, want %d (message cap)", len(got), historyMaxMessages)
	}
}

func TestBuildHistory_CharBudget(t *testing.T) {
	half := strings.Repeat("a", historyCharBudget/2-100)
	rows := []SessionMessage{
		{Role: "user", Content: "newest"},  // ~6
		{Role: "assistant", Content: half}, // ~9900 → total under budget
		{Role: "user", Content: half},      // ~9900 → total under budget
		{Role: "assistant", Content: half}, // pushes over → cut here
	}
	got := buildHistory(rows)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (char budget stops the 4th)", len(got))
	}
	// Chronological order: newest is last.
	if got[len(got)-1].Text != "newest" {
		t.Errorf("last msg = %q, want newest-last (chronological)", got[len(got)-1].Text)
	}
}

func TestBuildHistory_CacheBreakpointOnNewest(t *testing.T) {
	rows := []SessionMessage{
		{Role: "user", Content: "b"},
		{Role: "assistant", Content: "a"},
	}
	got := buildHistory(rows)
	if !got[len(got)-1].CacheBreakpoint {
		t.Error("expected cache breakpoint on the last (newest) message")
	}
	if got[0].CacheBreakpoint {
		t.Error("only the newest block may carry the breakpoint")
	}
}

func TestBuildHistory_Empty(t *testing.T) {
	if got := buildHistory(nil); len(got) != 0 {
		t.Fatalf("empty history should stay empty, got %d", len(got))
	}
}

func TestBuildSystemPrompt(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	got := buildSystemPrompt(PromptContext{Language: "he", OpenTasks: 7}, now)

	if !strings.HasPrefix(got, systemPromptBase) {
		t.Error("system prompt must start with the static base (cacheable prefix)")
	}
	for _, want := range []string{"2026-07-26", "he", "Open tasks: 7"} {
		if !strings.Contains(got, want) {
			t.Errorf("system prompt missing %q", want)
		}
	}
}

func TestBuildSystemPrompt_LanguageDefault(t *testing.T) {
	got := buildSystemPrompt(PromptContext{Language: "", OpenTasks: 0}, time.Now())
	if !strings.Contains(got, "en") {
		t.Error("empty language should default to en")
	}
}

// A user east of UTC has already crossed into the next local day while the
// server's UTC clock hasn't — "today" in the prompt must follow the user's
// zone, not the server's, or the assistant states the wrong calendar date.
func TestBuildSystemPrompt_UsesUserTimezoneNotUTC(t *testing.T) {
	// 22:00 UTC on the 25th is already 01:00 on the 26th in Asia/Jerusalem (UTC+3).
	now := time.Date(2026, 7, 25, 22, 0, 0, 0, time.UTC)
	got := buildSystemPrompt(PromptContext{Language: "en", Timezone: "Asia/Jerusalem"}, now)

	if !strings.Contains(got, "2026-07-26") {
		t.Errorf("expected the user's local date 2026-07-26, got: %s", got)
	}
	if strings.Contains(got, "2026-07-25") {
		t.Error("prompt still shows the server's UTC date instead of the user's local date")
	}
}

func TestBuildSystemPrompt_UnknownTimezoneFallsBackToUTC(t *testing.T) {
	now := time.Date(2026, 7, 26, 10, 0, 0, 0, time.UTC)
	got := buildSystemPrompt(PromptContext{Language: "en", Timezone: "Not/AZone"}, now)

	if !strings.Contains(got, "2026-07-26") {
		t.Errorf("unresolvable timezone should fall back to UTC, got: %s", got)
	}
}
