package recurrence

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Materializer turns due rules into task rows. One routine, two triggers:
//
//  1. the hourly cron sweep (RunAll), which catches every user; and
//  2. synchronously when a recurring task is completed, so an active user sees
//     the habit continue instantly even if the cron is broken.
//
// The horizon is deliberately **exactly one live instance per rule**. That is
// what bounds row growth and stops a daily rule from eating the project's
// active-task limit. Never materialize ahead.
type Materializer struct {
	repo        Repository
	now         func() time.Time
	broadcaster Broadcaster
	// taskLimit is the per-project active-task cap the insert must respect.
	// Injected rather than imported so recurrence never depends on the task
	// domain.
	taskLimit int
}

// NewMaterializer wires the sweep. broadcaster may be nil (emission disabled).
func NewMaterializer(repo Repository, broadcaster Broadcaster, taskLimit int) *Materializer {
	return &Materializer{repo: repo, now: time.Now, broadcaster: broadcaster, taskLimit: taskLimit}
}

// NewMaterializerWithClock is NewMaterializer with an injected clock, for
// deterministic tests of the due window.
func NewMaterializerWithClock(repo Repository, broadcaster Broadcaster, taskLimit int, now func() time.Time) *Materializer {
	return &Materializer{repo: repo, now: now, broadcaster: broadcaster, taskLimit: taskLimit}
}

// SweepResult is the run tally. It mirrors the notification sweeps' breakdown
// shape: a silent zero-run is exactly what hid the earlier staging cron failure,
// so "considered" is always reported alongside what actually happened.
type SweepResult struct {
	Considered       int `json:"considered"`
	Materialized     int `json:"materialized"`
	Reaped           int `json:"reaped"`
	SkippedPlanLimit int `json:"skippedPlanLimit"`
	SkippedExisting  int `json:"skippedExisting"`
	SkippedNotDue    int `json:"skippedNotDue"`
	SkippedBadZone   int `json:"skippedBadTimezone"`
}

// Run materializes every rule that has come due in its owner's local timezone.
// Idempotent: the partial unique index on (recurrence_rule_id, occurrence_date)
// means a second run within the same day creates nothing. dryRun computes the
// breakdown without writing.
func (m *Materializer) Run(ctx context.Context, dryRun bool) (*SweepResult, error) {
	due, err := m.repo.ListDue(ctx)
	if err != nil {
		return nil, fmt.Errorf("recurrence sweep: list due: %w", err)
	}

	nowUTC := m.now().UTC()
	res := &SweepResult{}

	for _, d := range due {
		res.Considered++

		today, ok := localToday(nowUTC, d.Timezone)
		if !ok {
			// A timezone that won't load is a data-integrity fault, not a normal
			// skip — surface it without a live DB probe.
			res.SkippedBadZone++
			log.Warn().Str("sweep", "recurrence").Str("rule_id", d.Rule.ID).
				Str("user_id", d.Rule.UserID).Str("timezone", d.Timezone).
				Msg("recurrence sweep skipped: timezone failed to load")
			continue
		}

		// The cursor is compared against the user's local today, so a rule fires on
		// their Monday, not UTC's.
		if d.Rule.NextOccurrence == nil || d.Rule.NextOccurrence.After(today) {
			res.SkippedNotDue++
			continue
		}

		if dryRun {
			res.Materialized++
			continue
		}

		out, err := m.materialize(ctx, d.Rule)
		if err != nil {
			return nil, err
		}
		applyResult(res, out)
	}

	logRun(res)
	return res, nil
}

// MaterializeAfterCompletion is trigger 2: a recurring task just reached `done`,
// so its successor is created in the same request. Best-effort by contract — the
// caller must not fail the status change if this errors, since the cron sweep
// will pick the rule up on the next tick.
func (m *Materializer) MaterializeAfterCompletion(ctx context.Context, userID, ruleID string) error {
	rule, err := m.repo.GetForMaterialize(ctx, userID, ruleID)
	if err != nil {
		return err
	}
	// A paused or exhausted rule has nothing to advance to.
	if rule.Paused || rule.NextOccurrence == nil {
		return nil
	}
	_, err = m.materialize(ctx, rule)
	return err
}

// materialize inserts the next occurrence, reaps the prior un-done instance, and
// advances the cursor — atomically, in the repository. The new cursor is computed
// here (the engine is pure) and handed down with the row.
func (m *Materializer) materialize(ctx context.Context, rule Rule) (MaterializeResult, error) {
	occDate := *rule.NextOccurrence

	// Advance past the instance being created now; nil when the series is spent.
	advanced := rule
	advanced.NextOccurrence = nil
	if next, ok := NextOccurrence(rule, occDate); ok {
		advanced.NextOccurrence = &next
	}

	occ := Occurrence{
		ID:               uuid.New().String(),
		UserID:           rule.UserID,
		ProjectID:        rule.ProjectID,
		RuleID:           rule.ID,
		Title:            rule.Title,
		Notes:            rule.Notes,
		Priority:         rule.Priority,
		Energy:           rule.Energy,
		EstimatedMinutes: rule.EstimatedMinutes,
		OccurrenceDate:   occDate,
	}

	out, err := m.repo.Materialize(ctx, advanced, occ, m.taskLimit)
	if err != nil {
		return MaterializeResult{}, fmt.Errorf("recurrence sweep: materialize %s: %w", rule.ID, err)
	}

	if out.SkippedPlanLimit {
		// Loud on purpose: the user silently stops receiving a habit they set up,
		// and the cursor stays put so it retries rather than losing the occurrence.
		log.Warn().Str("sweep", "recurrence").Str("rule_id", rule.ID).
			Str("user_id", rule.UserID).Str("project_id", rule.ProjectID).
			Msg("recurrence occurrence skipped: project at active-task limit — cursor not advanced")
	}
	if out.Created != nil {
		m.emit(rule.UserID, Event{Type: EventUpdated, Payload: ToView(advanced)})
	}
	return out, nil
}

func (m *Materializer) emit(userID string, ev Event) {
	if m.broadcaster != nil {
		m.broadcaster.Broadcast(userID, ev)
	}
}

func applyResult(res *SweepResult, out MaterializeResult) {
	switch {
	case out.SkippedPlanLimit:
		res.SkippedPlanLimit++
	case out.SkippedExisting:
		res.SkippedExisting++
	case out.Created != nil:
		res.Materialized++
	}
	if out.Reaped {
		res.Reaped++
	}
}

// logRun reports the sweep. A run that considered due rules but materialized
// nothing is logged at warn — `generated:0` in silence is what hid the earlier
// staging cron failure, and paused/exhausted rules never reach the due list, so
// this cannot cry wolf.
func logRun(res *SweepResult) {
	ev := log.Info()
	if res.Considered > 0 && res.Materialized == 0 && res.SkippedNotDue < res.Considered {
		ev = log.Warn()
	}
	ev.Str("sweep", "recurrence").
		Int("considered", res.Considered).
		Int("materialized", res.Materialized).
		Int("reaped", res.Reaped).
		Int("skipped_plan_limit", res.SkippedPlanLimit).
		Int("skipped_existing", res.SkippedExisting).
		Int("skipped_not_due", res.SkippedNotDue).
		Int("skipped_bad_timezone", res.SkippedBadZone).
		Msg("recurrence sweep complete")
}

// localToday is the user's current local date at UTC midnight, so it compares
// directly against a DATE cursor. ok=false means the stored zone won't load.
func localToday(nowUTC time.Time, tz string) (time.Time, bool) {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.Time{}, false
	}
	local := nowUTC.In(loc)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC), true
}
