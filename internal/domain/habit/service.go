package habit

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

type service struct {
	repo Repository
	bc   Broadcaster
	now  Clock
}

// NewService creates the habit service. bc may be nil — broadcasting is an
// optional seam and a nil Broadcaster is a valid no-op.
func NewService(repo Repository, bc Broadcaster) Service {
	return &service{repo: repo, bc: bc, now: time.Now}
}

// WithClock replaces the time source. Test-only seam: the local-date rules are
// only provable against a pinned instant, since the ambient answer is correct in
// a UTC container and wrong for a user thirteen hours away.
func (s *service) WithClock(c Clock) *service {
	s.now = c
	return s
}

func (s *service) emit(userID string, ev Event) {
	if s.bc != nil {
		s.bc.Broadcast(userID, ev)
	}
}

func invalid(msg string) error {
	return apperror.New(http.StatusUnprocessableEntity, apperror.ErrInvalidInput, msg)
}

func (s *service) List(ctx context.Context, userID string, includeArchived bool) ([]HabitView, error) {
	hs, err := s.repo.List(ctx, userID, includeArchived)
	if err != nil {
		return nil, err
	}
	out := make([]HabitView, 0, len(hs))
	for _, h := range hs {
		out = append(out, toView(h))
	}
	return out, nil
}

func (s *service) Get(ctx context.Context, userID, id string) (HabitView, error) {
	h, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		return HabitView{}, err
	}
	return toView(h), nil
}

func (s *service) Create(ctx context.Context, userID, plan string, req CreateHabitRequest) (HabitView, error) {
	name, err := validateName(req.Name)
	if err != nil {
		return HabitView{}, err
	}

	subject, err := validateSlug(req.Subject, DefaultSubject, MaxSubjectLen, "subject")
	if err != nil {
		return HabitView{}, err
	}
	color, err := validateSlug(req.Color, DefaultColor, MaxColorLen, "color")
	if err != nil {
		return HabitView{}, err
	}

	polarity := req.Polarity
	if polarity == "" {
		polarity = PolarityBuild
	}
	if polarity != PolarityBuild && polarity != PolarityQuit {
		return HabitView{}, invalid(`polarity must be "build" or "quit"`)
	}

	target := 1
	if req.TargetValue != nil {
		target = *req.TargetValue
	}
	if err := validateTarget(target, polarity); err != nil {
		return HabitView{}, err
	}

	unit, err := validateUnit(req.Unit)
	if err != nil {
		return HabitView{}, err
	}

	sched, err := validateSchedule(req.ScheduleKind, req.ByWeekday, req.TimesPerWeek)
	if err != nil {
		return HabitView{}, err
	}

	// Free users are capped on *active* habits. Plan comes from the JWT claim —
	// never a DB lookup. Archived habits do not count, so archiving frees a slot.
	if plan == "free" {
		count, err := s.repo.CountActive(ctx, userID)
		if err != nil {
			return HabitView{}, fmt.Errorf("habit.Create count: %w", err)
		}
		if count >= FreePlanHabitLimit {
			return HabitView{}, planLimitErr()
		}
	}

	h, err := s.repo.Create(ctx, Habit{
		ID:           uuid.New().String(),
		UserID:       userID,
		Name:         name,
		Subject:      subject,
		Color:        color,
		Polarity:     polarity,
		TargetValue:  target,
		Unit:         unit,
		ScheduleKind: sched.kind,
		ByWeekday:    sched.byWeekday,
		TimesPerWeek: sched.timesPerWeek,
	})
	if err != nil {
		return HabitView{}, err
	}

	view := toView(h)
	s.emit(userID, Event{Type: EventCreated, Payload: view})
	return view, nil
}

// Update merges the request onto the stored habit and writes the whole row.
func (s *service) Update(ctx context.Context, userID, plan, id string, req UpdateHabitRequest) (HabitView, error) {
	cur, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		return HabitView{}, err
	}

	p, err := mergeUpdate(cur, id, userID, req)
	if err != nil {
		return HabitView{}, err
	}

	// Restoring an archived habit consumes a plan slot, so it is gated exactly
	// like a create. Archiving is never gated.
	if req.Archived != nil && !*req.Archived && cur.ArchivedAt != nil && plan == "free" {
		count, err := s.repo.CountActive(ctx, userID)
		if err != nil {
			return HabitView{}, fmt.Errorf("habit.Update count: %w", err)
		}
		if count >= FreePlanHabitLimit {
			return HabitView{}, planLimitErr()
		}
	}

	h, ok, err := s.repo.Update(ctx, p)
	if err != nil {
		return HabitView{}, err
	}
	if !ok {
		return HabitView{}, notFound()
	}

	view := toView(h)
	s.emit(userID, Event{Type: EventUpdated, Payload: view})
	return view, nil
}

func (s *service) Delete(ctx context.Context, userID, id string) error {
	ok, err := s.repo.Archive(ctx, userID, id)
	if err != nil {
		return err
	}
	if !ok {
		return notFound()
	}
	s.emit(userID, Event{Type: EventDeleted, Payload: DeletedPayload{ID: id}})
	return nil
}

// CheckIn records or corrects one dated entry.
//
// The entry freezes the target it was judged by, so a later edit to the habit
// cannot rewrite what already happened. The write is an upsert on (habit, date):
// a double-tap updates the value instead of creating a second row, and the
// unique index — not a read-then-write — is what guarantees it.
func (s *service) CheckIn(ctx context.Context, userID, id string, req CheckInRequest) (HabitView, error) {
	h, today, err := s.habitAndToday(ctx, userID, id)
	if err != nil {
		return HabitView{}, err
	}

	date, err := s.resolveDate(h, req.Date, today)
	if err != nil {
		return HabitView{}, err
	}

	// Defaulting to the target means the common case — "I did it" — is an empty
	// body, and a binary habit never has to state that 1 means done.
	value := h.TargetValue
	if req.Value != nil {
		value = *req.Value
	}
	if value < 0 {
		return HabitView{}, invalid("value must be zero or greater")
	}

	if _, err := s.repo.UpsertCheckIn(ctx, CheckIn{
		ID:        uuid.New().String(),
		HabitID:   h.ID,
		UserID:    userID,
		Date:      date,
		Value:     value,
		TargetAt:  h.TargetValue,
		Satisfied: satisfies(h.Polarity, value, h.TargetValue),
	}); err != nil {
		return HabitView{}, err
	}

	view := toView(h)
	s.emit(userID, Event{Type: EventCheckedIn, Payload: view})
	return view, nil
}

// UndoCheckIn removes one dated entry. A date with no entry is not an error: the
// caller wanted the day not-done, and it already is. Undo has to be as cheap as
// the check-in, because a mis-tap on a grid of habit cards is routine.
func (s *service) UndoCheckIn(ctx context.Context, userID, id string, req UndoCheckInRequest) (HabitView, error) {
	h, today, err := s.habitAndToday(ctx, userID, id)
	if err != nil {
		return HabitView{}, err
	}

	date, err := s.resolveDate(h, req.Date, today)
	if err != nil {
		return HabitView{}, err
	}

	if _, err := s.repo.DeleteCheckIn(ctx, userID, h.ID, date); err != nil {
		return HabitView{}, err
	}

	view := toView(h)
	s.emit(userID, Event{Type: EventCheckedIn, Payload: view})
	return view, nil
}

// habitAndToday loads the habit and resolves the user's current local date.
// Archived habits are read-only history, not a live surface.
func (s *service) habitAndToday(ctx context.Context, userID, id string) (Habit, time.Time, error) {
	h, err := s.repo.GetByID(ctx, userID, id)
	if err != nil {
		return Habit{}, time.Time{}, err
	}
	if h.ArchivedAt != nil {
		return Habit{}, time.Time{}, invalid("cannot check in on an archived habit")
	}

	tz, err := s.repo.UserTimezone(ctx, userID)
	if err != nil {
		return Habit{}, time.Time{}, fmt.Errorf("habit.checkIn timezone: %w", err)
	}

	today := localDate(s.now(), loadLocation(tz), int(h.DayCutoffHour))
	return h, today, nil
}

// resolveDate turns an optional wire date into a calendar day. Omitted means
// today — computed here, never accepted from the client, because a supplied
// "today" is trivially spoofed to farm streaks and is wrong whenever a device
// clock drifts. A supplied date is a backfill and must sit inside the window.
func (s *service) resolveDate(h Habit, raw *string, today time.Time) (time.Time, error) {
	if raw == nil {
		return today, nil
	}
	date, err := parseDate(*raw)
	if err != nil {
		return time.Time{}, err
	}
	if err := validateCheckInDate(h, date, today); err != nil {
		return time.Time{}, err
	}
	return date, nil
}

func planLimitErr() error {
	return apperror.New(http.StatusForbidden, apperror.ErrPlanLimitExceeded,
		fmt.Sprintf("free plan allows up to %d active habits", FreePlanHabitLimit))
}

// mergeUpdate folds a partial request onto the stored habit, validating each
// field it touches. Merging here rather than in SQL is what lets the schedule be
// validated as a unit: switching to weekly_quota must clear by_weekday, and a
// column-wise UPDATE would leave the two fields describing different schedules.
func mergeUpdate(cur Habit, id, userID string, req UpdateHabitRequest) (UpdateParams, error) {
	p := UpdateParams{
		ID: id, UserID: userID,
		Name: cur.Name, Subject: cur.Subject, Color: cur.Color,
		TargetValue: cur.TargetValue, Unit: cur.Unit,
		ScheduleKind: cur.ScheduleKind, ByWeekday: cur.ByWeekday, TimesPerWeek: cur.TimesPerWeek,
		Archived: req.Archived,
	}

	if err := mergeScalars(&p, cur, req); err != nil {
		return UpdateParams{}, err
	}

	if req.ScheduleKind == nil && req.ByWeekday == nil && req.TimesPerWeek == nil {
		return p, nil
	}

	sched, err := mergeSchedule(cur, req)
	if err != nil {
		return UpdateParams{}, err
	}
	p.ScheduleKind, p.ByWeekday, p.TimesPerWeek = sched.kind, sched.byWeekday, sched.timesPerWeek

	// Mark the boundary so periods already scored under the old shape are left
	// alone. Schedule edits apply forward only.
	if scheduleMoved(cur, p) {
		now := time.Now().UTC()
		p.ScheduleChangedAt = &now
	}
	return p, nil
}

// mergeScalars applies the non-schedule fields of an edit, validating each one
// it touches and leaving the rest at their stored values.
func mergeScalars(p *UpdateParams, cur Habit, req UpdateHabitRequest) error {
	var err error
	if req.Name != nil {
		if p.Name, err = validateName(*req.Name); err != nil {
			return err
		}
	}
	if req.Subject != nil {
		if p.Subject, err = validateSlug(*req.Subject, DefaultSubject, MaxSubjectLen, "subject"); err != nil {
			return err
		}
	}
	if req.Color != nil {
		if p.Color, err = validateSlug(*req.Color, DefaultColor, MaxColorLen, "color"); err != nil {
			return err
		}
	}
	if req.Unit != nil {
		if p.Unit, err = validateUnit(req.Unit); err != nil {
			return err
		}
	}
	if req.TargetValue != nil {
		p.TargetValue = *req.TargetValue
	}
	// Judged against the *stored* polarity — the request cannot change it.
	return validateTarget(p.TargetValue, cur.Polarity)
}

// mergeSchedule resolves the incoming schedule fields against the stored ones.
// Fields carry over only when the kind is unchanged: switching kinds starts from
// a clean shape so a stale by_weekday cannot ride along.
func mergeSchedule(cur Habit, req UpdateHabitRequest) (schedule, error) {
	kind := cur.ScheduleKind
	if req.ScheduleKind != nil {
		kind = *req.ScheduleKind
	}

	byWeekday, timesPerWeek := req.ByWeekday, req.TimesPerWeek
	if kind == cur.ScheduleKind {
		if byWeekday == nil {
			byWeekday = cur.ByWeekday
		}
		if timesPerWeek == nil {
			timesPerWeek = cur.TimesPerWeek
		}
	}
	return validateSchedule(kind, byWeekday, timesPerWeek)
}

// scheduleMoved reports whether an edit changed the shape of the schedule, as
// opposed to touching only cosmetic fields.
func scheduleMoved(cur Habit, p UpdateParams) bool {
	if cur.ScheduleKind != p.ScheduleKind {
		return true
	}
	if len(cur.ByWeekday) != len(p.ByWeekday) {
		return true
	}
	for i := range cur.ByWeekday {
		if cur.ByWeekday[i] != p.ByWeekday[i] {
			return true
		}
	}
	switch {
	case cur.TimesPerWeek == nil && p.TimesPerWeek == nil:
		return false
	case cur.TimesPerWeek == nil || p.TimesPerWeek == nil:
		return true
	default:
		return *cur.TimesPerWeek != *p.TimesPerWeek
	}
}

func validateName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", invalid("name is required")
	}
	if len(name) > MaxNameLen {
		return "", invalid(fmt.Sprintf("name must be %d characters or fewer", MaxNameLen))
	}
	return name, nil
}

// validateSlug covers subject and colour: both are opaque keys the client maps
// to an icon or a swatch. They are deliberately not validated against a fixed
// list — the catalog is served (NIC-1926) and can gain entries without a
// backend deploy, and an unknown slug renders a fallback rather than breaking.
func validateSlug(raw, fallback string, maxLen int, field string) (string, error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return fallback, nil
	}
	if len(v) > maxLen {
		return "", invalid(fmt.Sprintf("%s must be %d characters or fewer", field, maxLen))
	}
	return v, nil
}

func validateUnit(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	u := strings.TrimSpace(*raw)
	if u == "" {
		return nil, nil
	}
	if len(u) > MaxUnitLen {
		return nil, invalid(fmt.Sprintf("unit must be %d characters or fewer", MaxUnitLen))
	}
	return &u, nil
}

// validateTarget guards the one combination that produces a habit nobody can
// ever fail: a build habit with a target of 0 is satisfied by doing nothing.
func validateTarget(target int, polarity string) error {
	if target < 0 {
		return invalid("targetValue must be zero or greater")
	}
	if polarity == PolarityBuild && target == 0 {
		return invalid("targetValue must be at least 1 for a build habit")
	}
	return nil
}

type schedule struct {
	kind         string
	byWeekday    []int16
	timesPerWeek *int16
}

// validateSchedule enforces the shape rule the database CHECK also holds: each
// kind requires its own fields and clears the others. Doing it here means a bad
// shape is a typed 422 rather than a 500 carrying a Postgres constraint name.
func validateSchedule(kind string, byWeekday []int16, timesPerWeek *int16) (schedule, error) {
	if kind == "" {
		kind = ScheduleDaily
	}

	switch kind {
	case ScheduleDaily:
		return schedule{kind: kind}, nil

	case ScheduleWeekdays:
		if len(byWeekday) == 0 {
			return schedule{}, invalid("byWeekday must contain at least one day for a weekdays habit")
		}
		seen := make(map[int16]bool, len(byWeekday))
		days := make([]int16, 0, len(byWeekday))
		for _, d := range byWeekday {
			if d < 0 || d > 6 {
				return schedule{}, invalid("byWeekday values must be between 0 (Sunday) and 6 (Saturday)")
			}
			if seen[d] {
				continue
			}
			seen[d] = true
			days = append(days, d)
		}
		return schedule{kind: kind, byWeekday: days}, nil

	case ScheduleWeeklyQuota:
		if timesPerWeek == nil {
			return schedule{}, invalid("timesPerWeek is required for a weekly quota habit")
		}
		if *timesPerWeek < 1 || *timesPerWeek > 7 {
			return schedule{}, invalid("timesPerWeek must be between 1 and 7")
		}
		return schedule{kind: kind, timesPerWeek: timesPerWeek}, nil

	default:
		return schedule{}, invalid(`scheduleKind must be "daily", "weekdays" or "weekly_quota"`)
	}
}
