package task

import (
	"sort"
	"time"
)

// FocusParams are the inputs to the focus scorer.
// Available == 0 means "no time budget given" (no over-budget filtering).
// Energy == "" means "no energy preference".
type FocusParams struct {
	Available int    // minutes the user has, 0 = unspecified
	Energy    string // low|medium|deep, "" = unspecified
	Limit     int    // already clamped by the service
}

// ── Scoring weights ──────────────────────────────────────────────────────────
// All scoring lives here as documented constants — no magic numbers scattered
// through the logic. Higher score ranks earlier. Tweaking these tunes the feel
// of /focus without touching the algorithm.
const (
	// Energy fit: how well the task's energy matches the user's current energy.
	scoreEnergyExact = 40 // task energy == requested energy
	scoreEnergyNear  = 15 // one step apart (low↔medium, medium↔deep)
	scoreEnergyFar   = 0  // two steps apart (low↔deep)

	// Time-budget fit: a task that comfortably fits the budget beats one that
	// only just fits. Tasks over budget are filtered out before scoring.
	scoreFitsComfortably = 20 // estimate ≤ 50% of available
	scoreFitsSnug        = 10 // estimate ≤ available
	scoreNoEstimate      = 5  // unknown duration — mild preference, never excluded

	// Due-date escalation: overdue is the loudest signal, then due soon.
	scoreOverdue  = 50
	scoreDueToday = 30
	scoreDueSoon  = 15 // within dueSoonDays
	dueSoonDays   = 3

	// Scheduled proximity: a task softly scheduled for today (incl. a rolled-over
	// past schedule still active) should surface.
	scoreScheduledToday = 25

	// Priority tiebreak — small, only meaningful when other signals tie.
	scorePriorityHigh   = 6
	scorePriorityMedium = 3
	scorePriorityLow    = 0
)

// energyRank maps energy to an ordinal so we can measure distance.
var energyRank = map[string]int{"low": 0, "medium": 1, "deep": 2}

// scoreTask computes the deterministic focus score for a single task at `now`.
// `now` is injected (never read inside) so tests are reproducible.
func scoreTask(t Task, p FocusParams, now time.Time) int {
	score := 0
	score += energyScore(t.Energy, p.Energy)
	score += budgetScore(t.EstimatedMinutes, p.Available)
	score += dueScore(t.DueDate, now)
	score += scheduleScore(t.ScheduledFor, t.RollsOver, now)
	score += priorityScore(t.Priority)
	return score
}

func energyScore(taskEnergy, want string) int {
	if want == "" {
		return 0
	}
	dist := abs(energyRank[taskEnergy] - energyRank[want])
	switch dist {
	case 0:
		return scoreEnergyExact
	case 1:
		return scoreEnergyNear
	default:
		return scoreEnergyFar
	}
}

func budgetScore(estimate *int, available int) int {
	if estimate == nil {
		return scoreNoEstimate
	}
	if available == 0 {
		return 0 // no budget given → estimate is neutral
	}
	if *estimate*2 <= available {
		return scoreFitsComfortably
	}
	return scoreFitsSnug
}

func dueScore(due *time.Time, now time.Time) int {
	if due == nil {
		return 0
	}
	today := dayStart(now)
	dueDay := dayStart(*due)
	switch {
	case dueDay.Before(today):
		return scoreOverdue
	case dueDay.Equal(today):
		return scoreDueToday
	case dueDay.Before(today.AddDate(0, 0, dueSoonDays+1)):
		return scoreDueSoon
	default:
		return 0
	}
}

func scheduleScore(scheduledFor *string, rollsOver bool, now time.Time) int {
	if scheduledFor == nil {
		return 0
	}
	sched, err := time.Parse(scheduledForLayout, *scheduledFor)
	if err != nil {
		return 0
	}
	today := dayStart(now)
	schedDay := dayStart(sched)
	// Scheduled for today, or a past schedule that rolls over → surfaces today.
	if schedDay.Equal(today) || (schedDay.Before(today) && rollsOver) {
		return scoreScheduledToday
	}
	return 0
}

func priorityScore(priority string) int {
	switch priority {
	case "high":
		return scorePriorityHigh
	case "medium":
		return scorePriorityMedium
	default:
		return scorePriorityLow
	}
}

// rankFocus filters the candidate set by the time budget, scores the rest, and
// returns them sorted best-first (ties broken by id for determinism), capped at
// the limit. Pure: same input + same `now` → same output.
func rankFocus(candidates []Task, p FocusParams, now time.Time) []Task {
	type scored struct {
		task  Task
		score int
	}
	ranked := make([]scored, 0, len(candidates))
	for _, t := range candidates {
		if p.Available > 0 && t.EstimatedMinutes != nil && *t.EstimatedMinutes > p.Available {
			continue // over budget — excluded
		}
		ranked = append(ranked, scored{task: t, score: scoreTask(t, p, now)})
	}

	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].task.ID < ranked[j].task.ID // deterministic tiebreak
	})

	if p.Limit > 0 && len(ranked) > p.Limit {
		ranked = ranked[:p.Limit]
	}
	out := make([]Task, len(ranked))
	for i, r := range ranked {
		out[i] = r.task
	}
	return out
}

func dayStart(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// NOTE (Phase 4, Pro): a future `?explain=true` will let a Pro user get an
// AI re-rank of this deterministic list with reasons. The deterministic engine
// here stays the Free baseline and the fallback when AI is unavailable. Do NOT
// build the AI path in this story.
