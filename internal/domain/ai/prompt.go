package ai

import (
	"encoding/json"
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
Be concise and actionable.

You have tools to read and to propose changes to the user's data:
- Read tools (list_tasks, get_task, list_projects) run inline. Use them to ground answers in real data instead of guessing.
- Write tools (complete_task, reschedule_task, create_task) do NOT execute directly. They open a confirmation card the user must approve. Never say a task is done, rescheduled, or created until the user confirms. If the user rejects a proposal, acknowledge and adjust; do not re-propose the same change immediately.

Prefer a quick read before proposing a write (e.g. list_projects before create_task, get_task before reschedule_task) so the proposal references real ids. Never invent task or project details you were not given.`

// buildSystemPrompt returns the static base followed by the volatile per-request
// context block. Keeping volatile data last preserves the cacheable prefix.
func buildSystemPrompt(pc PromptContext, now time.Time) string {
	lang := pc.Language
	if lang == "" {
		lang = "en"
	}
	loc, err := time.LoadLocation(pc.Timezone)
	if err != nil {
		loc = time.UTC
	}

	var b strings.Builder
	b.WriteString(systemPromptBase)
	b.WriteString("\n\n---\nContext:\n")
	// Weekday spelled out explicitly — an LLM deriving it from the ISO date
	// itself is unreliable and has been observed naming the wrong day.
	fmt.Fprintf(&b, "- Today's date: %s (%s)\n", now.In(loc).Format("2006-01-02"), now.In(loc).Format("Monday"))
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

// buildHistoryWithBlocks is buildHistory's tool-aware sibling. It preserves
// tool_use blocks (assistant turns) and tool_result blocks (user turns) so a
// multi-turn tool loop is faithful across retries and follow-ups.
func buildHistoryWithBlocks(rows []SessionMessage) []Message {
	var picked []SessionMessage
	total := 0
	for _, m := range rows {
		if len(picked) >= historyMaxMessages {
			break
		}
		// Approximate size by the visible content plus the raw block bytes;
		// tool payloads can be non-trivial and shouldn't sneak past the budget.
		total += len(m.Content) + len(m.ContentJSON)
		if total > historyCharBudget && len(picked) > 0 {
			break
		}
		picked = append(picked, m)
	}
	// Chronological (oldest-first) for the provider.
	msgs := make([]Message, len(picked))
	for i, m := range picked {
		msgs[len(picked)-1-i] = decodeHistoryMessage(m)
	}
	// Cache breakpoint on the newest included message.
	if len(msgs) > 0 {
		msgs[len(msgs)-1].CacheBreakpoint = true
	}
	return msgs
}

// decodeHistoryMessage turns one persisted turn back into a provider Message.
// If content_json is absent, we fall back to the plain text (pre-tool-loop rows
// stay compatible). If present, we parse the blocks and re-hydrate tool_use /
// tool_result faithfully.
func decodeHistoryMessage(m SessionMessage) Message {
	msg := Message{Role: Role(m.Role), Text: m.Content}
	if len(m.ContentJSON) == 0 {
		return msg
	}
	var blocks []struct {
		Type      string          `json:"type"`
		Text      string          `json:"text,omitempty"`
		ID        string          `json:"id,omitempty"`
		Name      string          `json:"name,omitempty"`
		Input     json.RawMessage `json:"input,omitempty"`
		ToolUseID string          `json:"tool_use_id,omitempty"`
		Content   string          `json:"content,omitempty"`
		IsError   bool            `json:"is_error,omitempty"`
	}
	if err := json.Unmarshal(m.ContentJSON, &blocks); err != nil {
		return msg // best-effort — fall back to plain text
	}
	// Recompute text from blocks so it can't drift from the stored JSON.
	var sb strings.Builder
	for _, b := range blocks {
		switch b.Type {
		case "text":
			sb.WriteString(b.Text)
		case "tool_use":
			msg.ToolUses = append(msg.ToolUses, ToolUseBlock{
				ID: b.ID, Name: b.Name, Input: b.Input,
			})
		case "tool_result":
			msg.ToolResults = append(msg.ToolResults, ToolResultBlock{
				ToolUseID: b.ToolUseID, Content: b.Content, IsError: b.IsError,
			})
		}
	}
	if txt := sb.String(); txt != "" {
		msg.Text = txt
	}
	return msg
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
