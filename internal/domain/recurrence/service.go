package recurrence

import (
	"context"
	"fmt"
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
}

// NewService creates a recurrence Service with a real clock. broadcaster may be
// nil (real-time emission disabled); pass the ws adapter to light up live updates.
func NewService(repo Repository, broadcaster Broadcaster) Service {
	return &service{repo: repo, now: time.Now, broadcaster: broadcaster}
}

// NewServiceWithClock is like NewService but with an injected clock, for
// deterministic tests of the cursor and the first-occurrence date.
func NewServiceWithClock(repo Repository, broadcaster Broadcaster, now func() time.Time) Service {
	return &service{repo: repo, now: now, broadcaster: broadcaster}
}

// emit fans a domain event out best-effort. A nil broadcaster is a valid no-op.
func (s *service) emit(userID string, ev Event) {
	if s.broadcaster != nil {
		s.broadcaster.Broadcast(userID, ev)
	}
}

func (s *service) Create(ctx context.Context, userID, projectID, plan string, req CreateRuleRequest) (RuleView, error) {
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
		return RuleView{}, err
	}
	if err := enforceTimedSchedulingPlan(plan, req.ScheduledTime); err != nil {
		return RuleView{}, err
	}
	req.EstimatedMinutes = clampEstimateToDayEnd(req.ScheduledTime, req.EstimatedMinutes)

	// Ownership before the plan count: a foreign project must not reveal how many
	// rules the caller has.
	owned, err := s.repo.ProjectOwned(ctx, userID, projectID)
	if err != nil {
		return RuleView{}, err
	}
	if !owned {
		return RuleView{}, apperror.New(http.StatusNotFound, apperror.ErrProjectNotFound, "project not found")
	}

	if plan == "free" {
		count, err := s.repo.CountByUser(ctx, userID)
		if err != nil {
			return RuleView{}, fmt.Errorf("recurrence.Create count: %w", err)
		}
		if count >= freePlanRuleLimit {
			return RuleView{}, apperror.New(http.StatusForbidden, apperror.ErrPlanLimitExceeded, "free plan allows up to 3 recurrence rules")
		}
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
		return RuleView{}, errInvalidRecurrence("the rule produces no occurrence before its end date")
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

	created, err := s.repo.CreateWithOccurrence(ctx, rule, occ)
	if err != nil {
		return RuleView{}, err
	}
	view := ToView(created)
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

	updated, scheduleChanged, err := applyUpdate(existing, req)
	if err != nil {
		return RuleView{}, err
	}

	if err := validateUpdate(updated); err != nil {
		return RuleView{}, err
	}

	// A schedule change invalidates the stored cursor — recompute it from the
	// same anchor the sweep will use, so the next fire lands on the new schedule.
	if scheduleChanged {
		anchor := dayOf(s.now().UTC()).AddDate(0, 0, -1)
		if updated.StartDate.After(anchor) {
			anchor = updated.StartDate.AddDate(0, 0, -1)
		}
		updated.NextOccurrence = nil
		if next, ok := NextOccurrence(updated, anchor); ok {
			updated.NextOccurrence = &next
		}
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

func (s *service) SetPaused(ctx context.Context, userID, id string, paused bool) (RuleView, error) {
	r, err := s.repo.SetPaused(ctx, userID, id, paused)
	if err != nil {
		return RuleView{}, err
	}
	view := ToView(r)
	s.emit(userID, Event{Type: EventUpdated, Payload: view})
	return view, nil
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
// today being unfinished is not a failure yet. `cancelled` also passes: the user
// deliberately opted out, which is not the same as letting the window lapse.
func currentStreak(newestFirst []string) int {
	streak := 0
	for _, st := range newestFirst {
		switch st {
		case StatusDone:
			streak++
		case StatusActive, StatusCancelled:
			continue
		default:
			return streak
		}
	}
	return streak
}

// Delete ends the series. Past occurrences are orphaned (FK ON DELETE SET NULL),
// never destroyed — they are the user's record of what they did.
func (s *service) Delete(ctx context.Context, userID, id string) error {
	if err := s.repo.Delete(ctx, userID, id); err != nil {
		return err
	}
	log.Debug().Str("rule_id", id).Msg("recurrence: series ended")
	s.emit(userID, Event{Type: EventDeleted, Payload: Ref{ID: id}})
	return nil
}
