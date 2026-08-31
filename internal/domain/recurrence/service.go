package recurrence

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

const (
	defaultPriority = "medium"
	defaultEnergy   = "medium"
)

type service struct {
	repo        Repository
	now         func() time.Time // injectable clock — the cursor is the only time read
	broadcaster Broadcaster      // nil disables emission
	taskReader  TaskTemplateReader
	// taskBroadcaster fires task.status_changed when SetPaused retires the live
	// occurrence. Reuses the same interface as Materializer. Nil disables emission.
	taskBroadcaster TaskEventBroadcaster
}

// NewService creates a recurrence Service with a real clock. broadcaster may be
// nil (real-time emission disabled); pass the ws adapter to light up live updates.
func NewService(repo Repository, broadcaster Broadcaster, taskReader TaskTemplateReader) Service {
	return &service{repo: repo, now: time.Now, broadcaster: broadcaster, taskReader: taskReader}
}

// NewServiceWithClock is like NewService but with an injected clock, for
// deterministic tests of the cursor and the first-occurrence date.
func NewServiceWithClock(
	repo Repository, broadcaster Broadcaster, taskReader TaskTemplateReader, now func() time.Time,
) Service {
	return &service{repo: repo, now: now, broadcaster: broadcaster, taskReader: taskReader}
}

// WithTaskBroadcaster injects the task-level WS emitter used when SetPaused
// retires the series' live occurrence. Nil is valid (emission disabled). Same
// post-construction pattern as task.Service's WithMaterializer.
func (s *service) WithTaskBroadcaster(tb TaskEventBroadcaster) Service {
	s.taskBroadcaster = tb
	return s
}

// emit fans a domain event out best-effort. A nil broadcaster is a valid no-op.
func (s *service) emit(userID string, ev Event) {
	if s.broadcaster != nil {
		s.broadcaster.Broadcast(userID, ev)
	}
}

func (s *service) Create(ctx context.Context, userID, projectID, plan string, req CreateRuleRequest) (RuleView, error) {
	result, err := s.createRule(ctx, userID, projectID, plan, req)
	if err != nil {
		return RuleView{}, err
	}
	return result.Rule, nil
}

func (s *service) CreateWithFirstTaskID(ctx context.Context, userID, projectID, plan string, req CreateRuleRequest) (CreateResult, error) {
	return s.createRule(ctx, userID, projectID, plan, req)
}

// createRule is the shared implementation behind Create and
// CreateWithFirstTaskID — one path, two public shapes.
func (s *service) createRule(ctx context.Context, userID, projectID, plan string, req CreateRuleRequest) (CreateResult, error) {
	req.Title = strings.TrimSpace(req.Title)
	if req.Priority == "" {
		req.Priority = defaultPriority
	}
	if req.Energy == "" {
		req.Energy = defaultEnergy
	}
	if req.Interval == 0 {
		req.Interval = 1
	}

	startDate, endDate, err := validateCreate(req)
	if err != nil {
		return CreateResult{}, err
	}
	if err := enforceTimedSchedulingPlan(plan, req.ScheduledTime); err != nil {
		return CreateResult{}, err
	}
	req.EstimatedMinutes = clampEstimateToDayEnd(req.ScheduledTime, req.EstimatedMinutes)

	// Ownership before the plan count: a foreign project must not reveal how many
	// rules the caller has.
	owned, err := s.repo.ProjectOwned(ctx, userID, projectID)
	if err != nil {
		return CreateResult{}, err
	}
	if !owned {
		return CreateResult{}, apperror.New(http.StatusNotFound, apperror.ErrProjectNotFound, "project not found")
	}

	// The actual cap is enforced atomically inside CreateWithOccurrence (a
	// per-user advisory lock guards the count-check + insert against a
	// concurrent create slipping through the same free slot). freeLimit <= 0
	// tells the repo to skip the guard entirely on Pro.
	freeLimit := 0
	if plan == planFree {
		freeLimit = freePlanRuleLimit
	}

	rule := Rule{
		ID:               uuid.New().String(),
		UserID:           userID,
		ProjectID:        projectID,
		Title:            req.Title,
		Notes:            req.Notes,
		Priority:         req.Priority,
		Energy:           req.Energy,
		EstimatedMinutes: req.EstimatedMinutes,
		ScheduledTime:    req.ScheduledTime,
		Freq:             req.Freq,
		Interval:         req.Interval,
		ByWeekday:        normalizeWeekdays(req.ByWeekday),
		ByMonthday:       req.ByMonthday,
		StartDate:        startDate,
		EndDate:          endDate,
	}

	// Instance #1 is the first occurrence on or after the start date. Seeking from
	// the day before start makes start itself eligible when it satisfies the
	// weekday/monthday constraint.
	first, ok := NextOccurrence(rule, startDate.AddDate(0, 0, -1))
	if !ok {
		return CreateResult{}, errInvalidRecurrence("the rule produces no occurrence before its end date")
	}

	// The cursor points at what comes *after* the instance being materialized now.
	if next, ok := NextOccurrence(rule, first); ok {
		rule.NextOccurrence = &next
	}

	occ := Occurrence{
		ID:               uuid.New().String(),
		UserID:           userID,
		ProjectID:        projectID,
		RuleID:           rule.ID,
		Title:            rule.Title,
		Notes:            rule.Notes,
		Priority:         rule.Priority,
		Energy:           rule.Energy,
		EstimatedMinutes: rule.EstimatedMinutes,
		ScheduledTime:    rule.ScheduledTime,
		OccurrenceDate:   first,
	}

	created, err := s.repo.CreateWithOccurrence(ctx, rule, occ, freeLimit)
	if err != nil {
		return CreateResult{}, err
	}
	view := ToView(created)
	s.emit(userID, Event{Type: EventCreated, Payload: view})
	return CreateResult{Rule: view, TaskID: occ.ID}, nil
}

// ConvertToRecurring turns an existing plain task into instance #1 of a new
// rule, in place. Template fields on req are ignored — they're read straight
// off the task row instead, so the produced rule can never drift from what's
// actually stored (see the Service interface doc for why).
func (s *service) ConvertToRecurring(
	ctx context.Context, userID, taskID, plan string, req CreateRuleRequest,
) (RuleView, error) {
	tmpl, err := s.taskReader.GetTemplate(ctx, userID, taskID)
	if err != nil {
		return RuleView{}, err
	}
	if tmpl.RecurrenceRuleID != nil {
		return RuleView{}, apperror.New(http.StatusConflict, apperror.ErrTaskAlreadyRecurring,
			"this task is already recurring — edit its existing rule instead")
	}
	// Only an active task can be promoted to a recurring instance. A done or
	// cancelled task would be silently resurrected by the ConvertTask SQL
	// (status='active' is forced unconditionally there), which is wrong (NIC-2000).
	if tmpl.Status != "active" {
		return RuleView{}, apperror.New(http.StatusConflict, apperror.ErrTaskNotActive,
			"only an active task can be converted to a recurring series (task status: "+tmpl.Status+")")
	}

	req.Title = tmpl.Title
	req.Notes = tmpl.Notes
	req.Priority = tmpl.Priority
	req.Energy = tmpl.Energy
	req.EstimatedMinutes = tmpl.EstimatedMinutes
	if req.Interval == 0 {
		req.Interval = 1
	}

	startDate, endDate, err := validateCreate(req)
	if err != nil {
		return RuleView{}, err
	}
	if err := enforceTimedSchedulingPlan(plan, req.ScheduledTime); err != nil {
		return RuleView{}, err
	}
	req.EstimatedMinutes = clampEstimateToDayEnd(req.ScheduledTime, req.EstimatedMinutes)

	freeLimit := 0
	if plan == planFree {
		freeLimit = freePlanRuleLimit
	}

	rule := Rule{
		ID:               uuid.New().String(),
		UserID:           userID,
		ProjectID:        tmpl.ProjectID,
		Title:            req.Title,
		Notes:            req.Notes,
		Priority:         req.Priority,
		Energy:           req.Energy,
		EstimatedMinutes: req.EstimatedMinutes,
		ScheduledTime:    req.ScheduledTime,
		Freq:             req.Freq,
		Interval:         req.Interval,
		ByWeekday:        normalizeWeekdays(req.ByWeekday),
		ByMonthday:       req.ByMonthday,
		StartDate:        startDate,
		EndDate:          endDate,
	}

	first, ok := NextOccurrence(rule, startDate.AddDate(0, 0, -1))
	if !ok {
		return RuleView{}, errInvalidRecurrence("the rule produces no occurrence before its end date")
	}
	if next, ok := NextOccurrence(rule, first); ok {
		rule.NextOccurrence = &next
	}

	converted, err := s.repo.ConvertTask(ctx, taskID, rule, first, freeLimit)
	if err != nil {
		return RuleView{}, err
	}
	view := ToView(converted)
	s.emit(userID, Event{Type: EventCreated, Payload: view})
	return view, nil
}

func (s *service) List(ctx context.Context, userID string, projectID *string) (ListRulesResponse, error) {
	rules, err := s.repo.List(ctx, userID, projectID)
	if err != nil {
		return ListRulesResponse{}, err
	}
	items := make([]RuleView, len(rules))
	for i, r := range rules {
		items[i] = ToView(r)
	}
	return ListRulesResponse{Items: items}, nil
}

func (s *service) Get(ctx context.Context, userID, id string) (RuleView, error) {
	r, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		return RuleView{}, err
	}
	return ToView(r), nil
}

// Update edits the series. Rule edits change the template for future instances;
// the repository re-stamps the single live instance in the same transaction.
// Editing an instance never propagates back to the rule.
func (s *service) Update(ctx context.Context, userID, id, plan string, req UpdateRuleRequest) (RuleView, error) {
	// Only a PATCH that actually sets a time is gated; an absent field and an
	// explicit null both leave a free user unblocked.
	if req.ScheduledTime.Set {
		if err := enforceTimedSchedulingPlan(plan, req.ScheduledTime.Value); err != nil {
			return RuleView{}, err
		}
	}

	existing, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		return RuleView{}, err
	}

	if err := s.enforceEditableOnFreePlan(ctx, userID, id, plan); err != nil {
		return RuleView{}, err
	}

	updated, scheduleChanged, err := applyUpdate(existing, req)
	if err != nil {
		return RuleView{}, err
	}

	if err := validateUpdate(updated); err != nil {
		return RuleView{}, err
	}

	// A schedule change invalidates the stored cursor — recompute from today.
	if scheduleChanged {
		updated.NextOccurrence = recomputeNextOccurrence(updated, s.now)
	}

	saved, err := s.repo.Update(ctx, updated)
	if err != nil {
		return RuleView{}, err
	}
	view := ToView(saved)
	s.emit(userID, Event{Type: EventUpdated, Payload: view})
	return view, nil
}

// applyUpdate folds the patch onto the existing rule and reports whether any
// schedule-bearing field moved (which forces a cursor recompute).
func applyUpdate(r Rule, req UpdateRuleRequest) (Rule, bool, error) {
	r = applyTemplatePatch(r, req)
	return applySchedulePatch(r, req)
}

// applyTemplatePatch folds the task-template fields onto the rule. None of them
// affect when the rule fires.
func applyTemplatePatch(r Rule, req UpdateRuleRequest) Rule {
	if req.Title != nil {
		r.Title = strings.TrimSpace(*req.Title)
	}
	if req.Notes.Set {
		r.Notes = req.Notes.Value
	}
	if req.Priority != nil {
		r.Priority = *req.Priority
	}
	if req.Energy != nil {
		r.Energy = *req.Energy
	}
	if req.EstimatedMinutes.Set {
		r.EstimatedMinutes = req.EstimatedMinutes.Value
	}
	if req.ScheduledTime.Set {
		r.ScheduledTime = req.ScheduledTime.Value
	}
	// Clamped against the EFFECTIVE pair: either side may be untouched by this
	// PATCH, so a new time must still respect an already-stored estimate.
	r.EstimatedMinutes = clampEstimateToDayEnd(r.ScheduledTime, r.EstimatedMinutes)
	return r
}

// applySchedulePatch folds the schedule fields on and reports whether any of them
// moved, which forces a cursor recompute.
func applySchedulePatch(r Rule, req UpdateRuleRequest) (Rule, bool, error) {
	scheduleChanged := false

	if req.Freq != nil && *req.Freq != r.Freq {
		r.Freq = *req.Freq
		scheduleChanged = true
	}
	if req.Interval != nil && *req.Interval != r.Interval {
		r.Interval = *req.Interval
		scheduleChanged = true
	}
	if req.ByWeekday != nil {
		r.ByWeekday = normalizeWeekdays(*req.ByWeekday)
		scheduleChanged = true
	}
	if req.ByMonthday.Set {
		r.ByMonthday = req.ByMonthday.Value
		scheduleChanged = true
	}
	if req.StartDate != nil {
		d, err := parseDate("startDate", *req.StartDate)
		if err != nil {
			return Rule{}, false, err
		}
		if !d.Equal(r.StartDate) {
			r.StartDate = d
			scheduleChanged = true
		}
	}
	if req.EndDate.Set {
		if req.EndDate.Value == nil {
			r.EndDate = nil
		} else {
			d, err := parseDate("endDate", *req.EndDate.Value)
			if err != nil {
				return Rule{}, false, err
			}
			r.EndDate = &d
		}
		scheduleChanged = true
	}

	// Clearing a constraint that no longer applies to the frequency keeps the
	// stored row consistent with what the engine will actually read.
	if r.Freq != FreqWeekly {
		r.ByWeekday = nil
	}
	if r.Freq != FreqMonthly {
		r.ByMonthday = nil
	}

	return r, scheduleChanged, nil
}

func (s *service) SetPaused(ctx context.Context, userID, id, plan string, paused bool) (RuleView, error) {
	// Pausing an over-cap rule is fine (it's the user reducing what fires), but
	// resuming one isn't — that's un-pausing a rule the free plan wouldn't let
	// them create today, so it's gated the same as any other schedule change.
	if !paused {
		if err := s.enforceEditableOnFreePlan(ctx, userID, id, plan); err != nil {
			return RuleView{}, err
		}
	}
	if paused {
		// Retire the live occurrence so it disappears from Today/TimeSpread the
		// moment the series is paused (NIC-2000). Best-effort: a failure is logged
		// but must not block the pause itself — the sweep won't produce a new
		// instance while the rule is paused, so the orphaned occurrence will be
		// reaped as missed on the next overdue pass if this ever fails.
		retiredID, retireErr := s.repo.RetireLiveInstance(ctx, userID, id)
		if retireErr != nil {
			log.Warn().Err(retireErr).Str("rule_id", id).Str("user_id", userID).
				Msg("recurrence: retire live instance on pause failed (best-effort)")
		} else if retiredID != "" && s.taskBroadcaster != nil {
			s.taskBroadcaster.BroadcastTaskStatusChanged(userID, retiredID)
		}
	}

	r, err := s.repo.SetPaused(ctx, userID, id, paused)
	if err != nil {
		return RuleView{}, err
	}

	if !paused {
		// Resume: the stored cursor may be stale (computed at pause time, against
		// an old "today"). Recompute from today so the first fire after resuming
		// lands on the correct next date (NIC-2000).
		r.NextOccurrence = recomputeNextOccurrence(r, s.now)
		saved, err := s.repo.Update(ctx, r)
		if err != nil {
			return RuleView{}, err
		}
		r = saved
	}

	view := ToView(r)
	s.emit(userID, Event{Type: EventUpdated, Payload: view})
	return view, nil
}

// recomputeNextOccurrence derives the next fire date from today's anchor using
// the same logic Update applies on a schedule change. Shared by Update and
// SetPaused's resume branch so the two can't drift apart (NIC-2000).
func recomputeNextOccurrence(r Rule, now func() time.Time) *time.Time {
	anchor := dayOf(now().UTC()).AddDate(0, 0, -1)
	if r.StartDate.After(anchor) {
		anchor = r.StartDate.AddDate(0, 0, -1)
	}
	if next, ok := NextOccurrence(r, anchor); ok {
		return &next
	}
	return nil
}

// enforceEditableOnFreePlan is the graceful-downgrade gate: a free-plan user
// keeps their oldest freePlanRuleLimit rules editable; anything past that cap
// (left over from a Pro downgrade) is read-only until deleted or they
// re-upgrade. Pro never hits this. The rule is assumed to already exist —
// callers check that via GetByID first, so a 404 never gets relabeled as a
// plan-limit error.
func (s *service) enforceEditableOnFreePlan(ctx context.Context, userID, id, plan string) error {
	if plan != planFree {
		return nil
	}
	within, err := s.repo.IsWithinFreeLimit(ctx, userID, id, freePlanRuleLimit)
	if err != nil {
		return err
	}
	if !within {
		return apperror.New(http.StatusForbidden, apperror.ErrPlanLimitExceeded,
			"this rule is over the free plan's 3-rule limit and is read-only — delete a rule or upgrade to edit it")
	}
	return nil
}

// Stats derives a rule's history from its occurrence rows. Nothing is stored: a
// counter column would count materializations, not completions, and drift the
// moment either trigger retries.
func (s *service) Stats(ctx context.Context, userID, id string) (StatsView, error) {
	// Scoped read first, so another user's rule 404s rather than leaking counts.
	if _, err := s.repo.GetByID(ctx, userID, id); err != nil {
		return StatsView{}, err
	}

	counts, err := s.repo.CountOccurrencesByStatus(ctx, userID, id)
	if err != nil {
		return StatsView{}, err
	}
	statuses, err := s.repo.ListOccurrenceStatuses(ctx, userID, id)
	if err != nil {
		return StatsView{}, err
	}

	return StatsView{
		Done:      counts[StatusDone],
		Missed:    counts[StatusMissed],
		Cancelled: counts[StatusCancelled],
		Streak:    currentStreak(statuses),
	}, nil
}

// currentStreak walks the occurrences newest-first and counts consecutive dones.
// The still-open instance (active) is skipped rather than breaking the streak —
// today being unfinished is not a failure yet. `cancelled` and `skipped` also
// pass: both are the user deliberately opting out, which is not the same as
// letting the window lapse.
func currentStreak(newestFirst []string) int {
	streak := 0
	for _, st := range newestFirst {
		switch st {
		case StatusDone:
			streak++
		case StatusActive, StatusCancelled, StatusSkipped:
			continue
		default:
			return streak
		}
	}
	return streak
}

// Delete ends the series. Every task the rule ever touched — past or the
// still-live one — is detached, never destroyed: they're the user's record
// of what they did (or are still doing).
func (s *service) Delete(ctx context.Context, userID, id string) error {
	if err := s.repo.Delete(ctx, userID, id); err != nil {
		return err
	}
	log.Debug().Str("rule_id", id).Msg("recurrence: series ended")
	s.emit(userID, Event{Type: EventDeleted, Payload: Ref{ID: id}})
	return nil
}
