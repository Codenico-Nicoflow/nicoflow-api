package recurrence

import (
	"context"
	"errors"
	"testing"
	"time"
)

// sweepRepo scripts the due list and records every Materialize call.
type sweepRepo struct {
	*fakeRepo
	due     []DueRule
	dueErr  error
	results map[string]MaterializeResult
	calls   []Occurrence
	advance map[string]*time.Time
	matErr  error
}

func newSweepRepo() *sweepRepo {
	return &sweepRepo{
		fakeRepo: newFakeRepo(),
		results:  map[string]MaterializeResult{},
		advance:  map[string]*time.Time{},
	}
}

func (s *sweepRepo) ListDue(context.Context) ([]DueRule, error) {
	if s.dueErr != nil {
		return nil, s.dueErr
	}
	return s.due, nil
}

func (s *sweepRepo) Materialize(_ context.Context, r Rule, occ Occurrence, _ int) (MaterializeResult, error) {
	if s.matErr != nil {
		return MaterializeResult{}, s.matErr
	}
	s.calls = append(s.calls, occ)
	s.advance[r.ID] = r.NextOccurrence
	if out, ok := s.results[r.ID]; ok {
		return out, nil
	}
	return MaterializeResult{Created: &occ}, nil
}

func dueRule(id, next, tz string) DueRule {
	n := date(next)
	return DueRule{
		Rule: Rule{
			ID: id, UserID: "u1", ProjectID: "p1", Title: "Water the plants",
			Priority: "medium", Energy: "medium",
			Freq: FreqDaily, Interval: 1, StartDate: date("2026-03-01"),
			NextOccurrence: &n,
		},
		Timezone: tz,
	}
}

func TestSweep_MaterializesDueRule(t *testing.T) {
	repo := newSweepRepo()
	repo.due = []DueRule{dueRule("r1", "2026-03-02", "UTC")}
	m := NewMaterializerWithClock(repo, nil, 50, fixedClock("2026-03-02"))

	res, err := m.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Considered != 1 || res.Materialized != 1 {
		t.Errorf("considered/materialized = %d/%d, want 1/1", res.Considered, res.Materialized)
	}
	if len(repo.calls) != 1 || FormatDate(repo.calls[0].OccurrenceDate) != "2026-03-02" {
		t.Fatalf("materialized occurrence = %+v, want one on 2026-03-02", repo.calls)
	}
	// The cursor handed down advances past the instance just created.
	if got := repo.advance["r1"]; got == nil || FormatDate(*got) != "2026-03-03" {
		t.Errorf("advanced cursor = %v, want 2026-03-03", got)
	}
	// The occurrence is stamped from the rule template.
	if repo.calls[0].Title != "Water the plants" {
		t.Errorf("occurrence title = %q, want the rule title", repo.calls[0].Title)
	}
}

// "Due" is decided in the user's local timezone, not UTC's.
func TestSweep_DueWindowIsPerUserTimezone(t *testing.T) {
	// 2026-03-02T23:30Z: already the 3rd in Asia/Jerusalem (+02), still the 2nd in UTC.
	nowUTC := func() time.Time { return time.Date(2026, 3, 2, 23, 30, 0, 0, time.UTC) }

	tests := []struct {
		name     string
		tz       string
		next     string
		wantFire bool
	}{
		{"ahead of UTC, cursor is local today", "Asia/Jerusalem", "2026-03-03", true},
		{"UTC, cursor is tomorrow", "UTC", "2026-03-03", false},
		{"behind UTC, cursor is local today", "America/New_York", "2026-03-02", true},
		{"cursor still in the future everywhere", "UTC", "2026-03-10", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newSweepRepo()
			repo.due = []DueRule{dueRule("r1", tt.next, tt.tz)}
			m := NewMaterializerWithClock(repo, nil, 50, nowUTC)

			res, err := m.Run(context.Background(), false)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got := res.Materialized == 1; got != tt.wantFire {
				t.Errorf("fired = %v, want %v (notDue=%d)", got, tt.wantFire, res.SkippedNotDue)
			}
		})
	}
}

// A cursor in the past (a missed tick, a cold start) still fires — the sweep
// catches up rather than losing the occurrence.
func TestSweep_PastCursorCatchesUp(t *testing.T) {
	repo := newSweepRepo()
	repo.due = []DueRule{dueRule("r1", "2026-02-20", "UTC")}
	m := NewMaterializerWithClock(repo, nil, 50, fixedClock("2026-03-02"))

	res, err := m.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Materialized != 1 {
		t.Errorf("materialized = %d, want 1", res.Materialized)
	}
	if FormatDate(repo.calls[0].OccurrenceDate) != "2026-02-20" {
		t.Errorf("occurrence date = %s, want the missed 2026-02-20", FormatDate(repo.calls[0].OccurrenceDate))
	}
}

// The plan-limit stall is the important failure mode: the cursor must NOT
// advance, or the occurrence is silently lost.
func TestSweep_PlanLimitStallIsCounted(t *testing.T) {
	repo := newSweepRepo()
	repo.due = []DueRule{dueRule("r1", "2026-03-02", "UTC")}
	repo.results["r1"] = MaterializeResult{SkippedPlanLimit: true}
	m := NewMaterializerWithClock(repo, nil, 50, fixedClock("2026-03-02"))

	res, err := m.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.SkippedPlanLimit != 1 {
		t.Errorf("skippedPlanLimit = %d, want 1", res.SkippedPlanLimit)
	}
	if res.Materialized != 0 {
		t.Errorf("materialized = %d, want 0", res.Materialized)
	}
}

func TestSweep_SkipsExistingLiveInstance(t *testing.T) {
	repo := newSweepRepo()
	repo.due = []DueRule{dueRule("r1", "2026-03-02", "UTC")}
	repo.results["r1"] = MaterializeResult{SkippedExisting: true}
	m := NewMaterializerWithClock(repo, nil, 50, fixedClock("2026-03-02"))

	res, err := m.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.SkippedExisting != 1 || res.Materialized != 0 {
		t.Errorf("existing/materialized = %d/%d, want 1/0", res.SkippedExisting, res.Materialized)
	}
}

func TestSweep_CountsReap(t *testing.T) {
	repo := newSweepRepo()
	repo.due = []DueRule{dueRule("r1", "2026-03-02", "UTC")}
	occ := Occurrence{ID: "t1"}
	repo.results["r1"] = MaterializeResult{Created: &occ, Reaped: true}
	m := NewMaterializerWithClock(repo, nil, 50, fixedClock("2026-03-02"))

	res, err := m.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Reaped != 1 || res.Materialized != 1 {
		t.Errorf("reaped/materialized = %d/%d, want 1/1", res.Reaped, res.Materialized)
	}
}

// A timezone that won't load is a data-integrity fault, bucketed separately so it
// can't hide inside "not due".
func TestSweep_BadTimezoneIsItsOwnBucket(t *testing.T) {
	repo := newSweepRepo()
	repo.due = []DueRule{dueRule("r1", "2026-03-02", "Mars/Olympus_Mons")}
	m := NewMaterializerWithClock(repo, nil, 50, fixedClock("2026-03-02"))

	res, err := m.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.SkippedBadZone != 1 {
		t.Errorf("skippedBadTimezone = %d, want 1", res.SkippedBadZone)
	}
	if len(repo.calls) != 0 {
		t.Errorf("materialize calls = %d, want 0", len(repo.calls))
	}
}

// dryRun reports what would happen and writes nothing.
func TestSweep_DryRunWritesNothing(t *testing.T) {
	repo := newSweepRepo()
	repo.due = []DueRule{dueRule("r1", "2026-03-02", "UTC")}
	m := NewMaterializerWithClock(repo, nil, 50, fixedClock("2026-03-02"))

	res, err := m.Run(context.Background(), true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Materialized != 1 {
		t.Errorf("materialized = %d, want 1 (projected)", res.Materialized)
	}
	if len(repo.calls) != 0 {
		t.Errorf("materialize calls = %d, want 0 under dryRun", len(repo.calls))
	}
}

// An exhausted series hands down a nil cursor rather than a date past end_date.
func TestSweep_ExhaustedSeriesNullsCursor(t *testing.T) {
	repo := newSweepRepo()
	d := dueRule("r1", "2026-03-02", "UTC")
	end := date("2026-03-02")
	d.Rule.EndDate = &end
	repo.due = []DueRule{d}
	m := NewMaterializerWithClock(repo, nil, 50, fixedClock("2026-03-02"))

	if _, err := m.Run(context.Background(), false); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, ok := repo.advance["r1"]; !ok || got != nil {
		t.Errorf("advanced cursor = %v, want nil (series exhausted)", got)
	}
}

func TestSweep_EmptyDueListIsNotAnError(t *testing.T) {
	m := NewMaterializerWithClock(newSweepRepo(), nil, 50, fixedClock("2026-03-02"))
	res, err := m.Run(context.Background(), false)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Considered != 0 || res.Materialized != 0 {
		t.Errorf("res = %+v, want an all-zero tally", res)
	}
}

func TestSweep_PropagatesRepoErrors(t *testing.T) {
	t.Run("list", func(t *testing.T) {
		repo := newSweepRepo()
		repo.dueErr = errors.New("db down")
		m := NewMaterializerWithClock(repo, nil, 50, fixedClock("2026-03-02"))
		if _, err := m.Run(context.Background(), false); err == nil {
			t.Fatal("err = nil, want the list failure")
		}
	})
	t.Run("materialize", func(t *testing.T) {
		repo := newSweepRepo()
		repo.due = []DueRule{dueRule("r1", "2026-03-02", "UTC")}
		repo.matErr = errors.New("db down")
		m := NewMaterializerWithClock(repo, nil, 50, fixedClock("2026-03-02"))
		if _, err := m.Run(context.Background(), false); err == nil {
			t.Fatal("err = nil, want the materialize failure")
		}
	})
}

// Trigger 2: completing an occurrence creates its successor immediately.
func TestMaterializeAfterCompletion(t *testing.T) {
	repo := newSweepRepo()
	rule := dueRule("r1", "2026-03-03", "UTC").Rule
	repo.rules[rule.ID] = rule
	m := NewMaterializerWithClock(repo, nil, 50, fixedClock("2026-03-02"))

	if err := m.MaterializeAfterCompletion(context.Background(), "u1", "r1"); err != nil {
		t.Fatalf("MaterializeAfterCompletion: %v", err)
	}
	if len(repo.calls) != 1 || FormatDate(repo.calls[0].OccurrenceDate) != "2026-03-03" {
		t.Errorf("calls = %+v, want one occurrence on 2026-03-03", repo.calls)
	}
}

// A paused or exhausted rule has nothing to advance to — no successor, no error.
func TestMaterializeAfterCompletion_NoOpCases(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Rule)
	}{
		{"paused", func(r *Rule) { r.Paused = true }},
		{"exhausted", func(r *Rule) { r.NextOccurrence = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newSweepRepo()
			rule := dueRule("r1", "2026-03-03", "UTC").Rule
			tt.mutate(&rule)
			repo.rules[rule.ID] = rule
			m := NewMaterializerWithClock(repo, nil, 50, fixedClock("2026-03-02"))

			if err := m.MaterializeAfterCompletion(context.Background(), "u1", "r1"); err != nil {
				t.Fatalf("MaterializeAfterCompletion: %v", err)
			}
			if len(repo.calls) != 0 {
				t.Errorf("calls = %d, want 0", len(repo.calls))
			}
		})
	}
}

// Another user's rule 404s rather than materializing.
func TestMaterializeAfterCompletion_RowLevelIsolation(t *testing.T) {
	repo := newSweepRepo()
	rule := dueRule("r1", "2026-03-03", "UTC").Rule
	repo.rules[rule.ID] = rule
	m := NewMaterializerWithClock(repo, nil, 50, fixedClock("2026-03-02"))

	err := m.MaterializeAfterCompletion(context.Background(), "u2", "r1")
	assertCode(t, err, "RECURRENCE_RULE_NOT_FOUND")
	if len(repo.calls) != 0 {
		t.Errorf("calls = %d, want 0", len(repo.calls))
	}
}

func TestLocalToday(t *testing.T) {
	nowUTC := time.Date(2026, 3, 2, 23, 30, 0, 0, time.UTC)
	tests := []struct {
		name, tz, want string
		ok             bool
	}{
		{"utc", "UTC", "2026-03-02", true},
		{"ahead rolls to tomorrow", "Asia/Jerusalem", "2026-03-03", true},
		{"behind stays on today", "America/New_York", "2026-03-02", true},
		{"unloadable zone", "Nowhere/Bad", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := localToday(nowUTC, tt.tz)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && FormatDate(got) != tt.want {
				t.Errorf("localToday = %s, want %s", FormatDate(got), tt.want)
			}
		})
	}
}

func TestCurrentStreak(t *testing.T) {
	tests := []struct {
		name     string
		statuses []string
		want     int
	}{
		{"no occurrences", nil, 0},
		{"all done", []string{StatusDone, StatusDone, StatusDone}, 3},
		{"broken by missed", []string{StatusDone, StatusDone, StatusMissed, StatusDone}, 2},
		{"open instance does not break it", []string{StatusActive, StatusDone, StatusDone}, 2},
		{"cancelled is skipped, not a break", []string{StatusDone, StatusCancelled, StatusDone}, 2},
		{"most recent missed means no streak", []string{StatusMissed, StatusDone, StatusDone}, 0},
		{"only an open instance", []string{StatusActive}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := currentStreak(tt.statuses); got != tt.want {
				t.Errorf("currentStreak = %d, want %d", got, tt.want)
			}
		})
	}
}
