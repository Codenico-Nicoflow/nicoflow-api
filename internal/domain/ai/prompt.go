package ai

import (
	"fmt"
	"strings"
	"time"
)

const (
	// historyCharBudget bounds the built history by total content characters,
	// newest-first. A char proxy (no tokenizer dep) — ~len/3.5 ≈ tokens.
	historyCharBudget = 20000
	// historyMaxMessages hard-caps the number of history turns regardless of size.
	historyMaxMessages = 20
	// titleMaxLen is the first-message-derived title ceiling (word-boundary cut).
	titleMaxLen = 50
)

// systemPromptBase is the static persona + scope guard. Volatile context
// (language, date, open-task count) is appended last so the cached prefix — the
// base — stays stable across turns.
const systemPromptBase = `You are Nicoflow's assistant, a focused productivity companion inside a GTD-inspired task manager.
Help the user capture, clarify, organise, and plan their work: tasks, projects, and areas of responsibility.
Stay on productivity and task-management topics; if asked to do something unrelated, gently steer back.
Be concise and actionable. You cannot read or modify the user's tasks directly — advise, don't claim to act.
Never invent task or project details you were not given.`

// buildSystemPrompt returns the static base followed by the volatile per-request
// context block. Keeping volatile data last preserves the cacheable prefix.
func buildSystemPrompt(pc PromptContext, now time.Time) string {
	lang := pc.Language
	if lang == "" {
		lang = "en"
	}
	var b strings.Builder
	b.WriteString(systemPromptBase)
	b.WriteString("\n\n---\nContext:\n")
	fmt.Fprintf(&b, "- Today's date: %s\n", now.UTC().Format("2006-01-02"))
	fmt.Fprintf(&b, "- User's preferred language: %s (reply in this language)\n", lang)
	fmt.Fprintf(&b, "- Open tasks: %d", pc.OpenTasks)
	return b.String()
}

// buildHistory turns newest-first stored rows into oldest-first provider
// messages within the char + message-count budget, and marks the final (newest)
// included message as the cache breakpoint. rows must be ordered newest-first.
func buildHistory(rows []SessionMessage) []Message {
	var picked []SessionMessage
	total := 0
	for _, m := range rows {
		if len(picked) >= historyMaxMessages {
			break
		}
		total += len(m.Content)
		if total > historyCharBudget && len(picked) > 0 {
			break
		}
		picked = append(picked, m)
	}

	// Reverse to chronological (oldest-first) for the provider.
	msgs := make([]Message, len(picked))
	for i, m := range picked {
		msgs[len(picked)-1-i] = Message{Role: Role(m.Role), Text: m.Content}
	}
	// Breakpoint on the last (newest) block — the longest reusable prefix.
	if len(msgs) > 0 {
		msgs[len(msgs)-1].CacheBreakpoint = true
	}
	return msgs
}

// deriveTitle is the first-message title: trimmed, word-boundary-truncated to
// titleMaxLen. A single word longer than the cap is hard-cut.
func deriveTitle(content string) string {
	content = strings.TrimSpace(content)
	if len(content) <= titleMaxLen {
		return content
	}
	cut := content[:titleMaxLen]
	if i := strings.LastIndexByte(cut, ' '); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut)
}
