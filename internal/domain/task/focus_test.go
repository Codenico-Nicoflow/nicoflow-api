package task

import (
	"testing"
	"time"
)

// fixedNow is the injected clock for all deterministic scorer tests.
var fixedNow = time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)

func task(id, energy, priority string, est *int) Task {
	return Task{ID: id, Energy: energy, Priority: priority, EstimatedMinutes: est, Status: "active"}
}

func intp(n int) *int { return &n }

func TestEnergyScore(t *testing.T) {
	tests := []struct {
		taskEnergy, want string
		exp              int
	}{
		{"low", "low", scoreEnergyExact},
		{"medium", "low", scoreEnergyNear},
		{"deep", "low", scoreEnergyFar},
		{"deep", "medium", scoreEnergyNear},
		{"low", "", 0}, // no preference
	}
	for _, tt := range tests {
		if got := energyScore(tt.taskEnergy, tt.want); got != tt.exp {
			t.Errorf("energyScore(%q,%q) = %d, want %d", tt.taskEnergy, tt.want, got, tt.exp)
		}
	}
}

func TestBudgetScore(t *testing.T) {
	tests := []struct {
		est       *int
		available int
		exp       int
	}{
		{nil, 60, scoreNoEstimate},           // unknown estimate
		{intp(20), 60, scoreFitsComfortably}, // 20 ≤ 50% of 60
		{intp(50), 60, scoreFitsSnug},        // fits but not comfortably
		{intp(30), 0, 0},                     // no budget given → neutral
	}
	for _, tt := range tests {
		if got := budgetScore(tt.est, tt.available); got != tt.exp {
			t.Errorf("budgetScore(%v,%d) = %d, want %d", tt.est, tt.available, got, tt.exp)
		}
	}
}

func TestScheduleScore(t *testing.T) {
	day := func(d int) *string {
		s := fixedNow.AddDate(0, 0, d).Format(scheduledForLayout)
		return &s
	}
	tests := []struct {
		name      string
		scheduled *string
		rollsOver bool
		exp       int
	}{
		{"unscheduled", nil, true, 0},
		{"scheduled today", day(0), true, scoreScheduledToday},
		{"past + rollsOver escalates", day(-2), true, scoreScheduledOverdue},
		{"past + no rollsOver", day(-2), false, 0},
		{"scheduled soon", day(2), true, scoreScheduledSoon},
		{"future far", day(10), true, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scheduleScore(tt.scheduled, tt.rollsOver, fixedNow); got != tt.exp {
				t.Errorf("scheduleScore = %d, want %d", got, tt.exp)
			}
		})
	}
}

func TestRankFocus_OverBudgetExcluded(t *testing.T) {
	candidates := []Task{
		task("a", "low", "low", intp(90)), // over a 30-min budget
		task("b", "low", "low", intp(20)),
	}
	got := rankFocus(candidates, FocusParams{Available: 30}, fixedNow)
	if len(got) != 1 || got[0].ID != "b" {
		t.Fatalf("over-budget task should be excluded; got %+v", ids(got))
	}
}

func TestRankFocus_EnergyRanking(t *testing.T) {
	candidates := []Task{
		task("deep", "deep", "low", intp(20)),
		task("low", "low", "low", intp(20)),
	}
	got := rankFocus(candidates, FocusParams{Available: 60, Energy: "low"}, fixedNow)
	if got[0].ID != "low" {
		t.Errorf("low-energy task should rank first; got %v", ids(got))
	}
}

func TestRankFocus_DeterministicAndLimited(t *testing.T) {
	candidates := []Task{
		task("c", "low", "low", intp(10)),
		task("a", "low", "low", intp(10)),
		task("b", "low", "low", intp(10)),
	}
	first := rankFocus(candidates, FocusParams{Available: 60, Limit: 2}, fixedNow)
	second := rankFocus(candidates, FocusParams{Available: 60, Limit: 2}, fixedNow)
	if len(first) != 2 {
		t.Fatalf("limit not applied: len=%d", len(first))
	}
	if ids(first) != ids(second) {
		t.Errorf("non-deterministic: %v vs %v", ids(first), ids(second))
	}
	// equal scores → tiebreak by id asc → a,b
	if first[0].ID != "a" || first[1].ID != "b" {
		t.Errorf("tiebreak wrong: %v", ids(first))
	}
}

func ids(ts []Task) string {
	s := ""
	for _, t := range ts {
		s += t.ID + ","
	}
	return s
}
