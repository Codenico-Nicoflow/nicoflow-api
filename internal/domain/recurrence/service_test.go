package recurrence

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/optional"
)

// fakeRepo records what the service asked for and returns scripted results.
type fakeRepo struct {
	rules        map[string]Rule
	count        int
	projectOwned bool

	createdOcc   Occurrence
	deletedID    string
	createErr    error
	statusCounts map[string]int
	statuses     []string
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{rules: map[string]Rule{}, projectOwned: true}
}

func (f *fakeRepo) CreateWithOccurrence(_ context.Context, r Rule, occ Occurrence) (Rule, error) {
	if f.createErr != nil {
		return Rule{}, f.createErr
	}
	f.createdOcc = occ
	r.CreatedAt, r.UpdatedAt = time.Now(), time.Now()
	f.rules[r.ID] = r
	return r, nil
}

func (f *fakeRepo) List(_ context.Context, _ string, projectID *string) ([]Rule, error) {
	var out []Rule
	for _, r := range f.rules {
		if projectID == nil || r.ProjectID == *projectID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeRepo) GetByID(_ context.Context, userID, id string) (Rule, error) {
	r, ok := f.rules[id]
	if !ok || r.UserID != userID {
		return Rule{}, errRuleNotFound()
	}
	return r, nil
}

func (f *fakeRepo) Update(_ context.Context, r Rule) (Rule, error) {
	f.rules[r.ID] = r
	return r, nil
}

func (f *fakeRepo) SetPaused(_ context.Context, userID, id string, paused bool) (Rule, error) {
	r, ok := f.rules[id]
	if !ok || r.UserID != userID {
		return Rule{}, errRuleNotFound()
	}
	r.Paused = paused
	f.rules[id] = r
	return r, nil
}

func (f *fakeRepo) Delete(_ context.Context, userID, id string) error {
	r, ok := f.rules[id]
	if !ok || r.UserID != userID {
		return errRuleNotFound()
	}
	f.deletedID = id
	delete(f.rules, id)
	return nil
}

func (f *fakeRepo) CountByUser(context.Context, string) (int, error) { return f.count, nil }

func (f *fakeRepo) ListDue(context.Context) ([]DueRule, error) { return nil, nil }

func (f *fakeRepo) GetForMaterialize(ctx context.Context, userID, ruleID string) (Rule, error) {
	return f.GetByID(ctx, userID, ruleID)
}

func (f *fakeRepo) Materialize(_ context.Context, _ Rule, occ Occurrence, _ int) (MaterializeResult, error) {
	return MaterializeResult{Created: &occ}, nil
}

func (f *fakeRepo) CountOccurrencesByStatus(context.Context, string, string) (map[string]int, error) {
	return f.statusCounts, nil
}

func (f *fakeRepo) ListOccurrenceStatuses(context.Context, string, string) ([]string, error) {
	return f.statuses, nil
}
func (f *fakeRepo) ProjectOwned(context.Context, string, string) (bool, error) {
	return f.projectOwned, nil
}

// recorder captures emitted events so tests can assert on real-time fan-out.
type recorder struct{ events []Event }

func (r *recorder) Broadcast(_ string, ev Event) { r.events = append(r.events, ev) }

func fixedClock(s string) func() time.Time {
	return func() time.Time { return date(s) }
}

func validCreate() CreateRuleRequest {
	return CreateRuleRequest{
		Title:     "Water the plants",
		Freq:      FreqWeekly,
		Interval:  1,
		ByWeekday: []int{1},
		StartDate: "2026-03-02",
	}
}

// assertCode fails unless err is an AppError carrying the wanted code.
func assertCode(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("err = nil, want %s", want)
	}
	var ae *apperror.AppError
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v, want *apperror.AppError with code %s", err, want)
	}
	if ae.Code != want {
		t.Errorf("code = %s, want %s", ae.Code, want)
	}
}

func TestCreate_MaterializesFirstOccurrence(t *testing.T) {
	repo := newFakeRepo()
	rec := &recorder{}
	svc := NewServiceWithClock(repo, rec, fixedClock("2026-03-01"))

	view, err := svc.Create(context.Background(), "u1", "p1", "free", validCreate())
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Instance #1 is the start date itself (a Monday, which is the selected day).
	if got := FormatDate(repo.createdOcc.OccurrenceDate); got != "2026-03-02" {
		t.Errorf("occurrence date = %s, want 2026-03-02", got)
	}
	if repo.createdOcc.RuleID != view.ID {
		t.Errorf("occurrence rule id = %s, want %s", repo.createdOcc.RuleID, view.ID)
	}
	// The cursor points past the materialized instance, not at it.
	if view.NextOccurrence == nil || *view.NextOccurrence != "2026-03-09" {
		t.Errorf("nextOccurrence = %v, want 2026-03-09", view.NextOccurrence)
	}
	// The occurrence is stamped from the template.
	if repo.createdOcc.Title != "Water the plants" {
		t.Errorf("occurrence title = %q, want the rule title", repo.createdOcc.Title)
	}
}

func TestCreate_PlanLimit(t *testing.T) {
	tests := []struct {
		name      string
		plan      string
		existing  int
		wantError bool
	}{
		{"free under the cap", "free", 2, false},
		{"free at the cap", "free", 3, true},
		{"free past the cap", "free", 4, true},
		{"pro at the free cap", "pro", 3, false},
		{"pro far past the free cap", "pro", 99, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newFakeRepo()
			repo.count = tt.existing
			svc := NewServiceWithClock(repo, nil, fixedClock("2026-03-01"))

			_, err := svc.Create(context.Background(), "u1", "p1", tt.plan, validCreate())
			if tt.wantError {
				assertCode(t, err, apperror.ErrPlanLimitExceeded)
				return
			}
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
		})
	}
}

// A foreign project must 404 before the plan count runs, so it can't be used to
// probe how many rules the caller owns.
func TestCreate_ForeignProjectNotFound(t *testing.T) {
	repo := newFakeRepo()
	repo.projectOwned = false
	repo.count = 99
	svc := NewServiceWithClock(repo, nil, fixedClock("2026-03-01"))

	_, err := svc.Create(context.Background(), "u1", "p-other", "free", validCreate())
	assertCode(t, err, apperror.ErrProjectNotFound)
}

func TestCreate_Validation(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*CreateRuleRequest)
		wantCode string
	}{
		{"empty title", func(r *CreateRuleRequest) { r.Title = "  " }, apperror.ErrInvalidInput},
		{"unknown freq", func(r *CreateRuleRequest) { r.Freq = "fortnightly" }, apperror.ErrInvalidRecurrence},
		{"interval zero is defaulted, not rejected", nil, ""},
		{"interval too large", func(r *CreateRuleRequest) { r.Interval = 400 }, apperror.ErrInvalidRecurrence},
		{"interval negative", func(r *CreateRuleRequest) { r.Interval = -1 }, apperror.ErrInvalidRecurrence},
		{"weekday out of range", func(r *CreateRuleRequest) { r.ByWeekday = []int{7} }, apperror.ErrInvalidRecurrence},
		{"duplicate weekday", func(r *CreateRuleRequest) { r.ByWeekday = []int{1, 1} }, apperror.ErrInvalidRecurrence},
		{"weekday on a daily rule", func(r *CreateRuleRequest) {
			r.Freq, r.ByWeekday = FreqDaily, []int{1}
		}, apperror.ErrInvalidRecurrence},
		{"monthday on a weekly rule", func(r *CreateRuleRequest) { r.ByMonthday = ptrInt(15) }, apperror.ErrInvalidRecurrence},
		{"monthday out of range", func(r *CreateRuleRequest) {
			r.Freq, r.ByWeekday, r.ByMonthday = FreqMonthly, nil, ptrInt(32)
		}, apperror.ErrInvalidRecurrence},
		{"monthday zero", func(r *CreateRuleRequest) {
			r.Freq, r.ByWeekday, r.ByMonthday = FreqMonthly, nil, ptrInt(0)
		}, apperror.ErrInvalidRecurrence},
		{"malformed start date", func(r *CreateRuleRequest) { r.StartDate = "03/02/2026" }, apperror.ErrInvalidDate},
		{"end before start", func(r *CreateRuleRequest) {
			end := "2026-03-01"
			r.EndDate = &end
		}, apperror.ErrInvalidRecurrence},
		{"bad priority", func(r *CreateRuleRequest) { r.Priority = "urgent" }, apperror.ErrInvalidInput},
		{"bad energy", func(r *CreateRuleRequest) { r.Energy = "extreme" }, apperror.ErrInvalidInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := validCreate()
			if tt.mutate != nil {
				tt.mutate(&req)
			} else {
				req.Interval = 0
			}
			svc := NewServiceWithClock(newFakeRepo(), nil, fixedClock("2026-03-01"))

			_, err := svc.Create(context.Background(), "u1", "p1", "free", req)
			if tt.wantCode == "" {
				if err != nil {
					t.Fatalf("Create: %v", err)
				}
				return
			}
			assertCode(t, err, tt.wantCode)
		})
	}
}

// -1 (last day of month) is the one out-of-1..31 monthday that is legal.
func TestCreate_MonthdayLastIsAccepted(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, nil, fixedClock("2026-01-01"))

	req := validCreate()
	req.Freq, req.ByWeekday, req.ByMonthday = FreqMonthly, nil, ptrInt(MonthdayLast)
	req.StartDate = "2026-01-01"

	if _, err := svc.Create(context.Background(), "u1", "p1", "free", req); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got := FormatDate(repo.createdOcc.OccurrenceDate); got != "2026-01-31" {
		t.Errorf("occurrence date = %s, want 2026-01-31", got)
	}
}

// A window too narrow to contain any occurrence is rejected rather than stored
// as a rule that can never fire.
func TestCreate_NoOccurrenceInWindow(t *testing.T) {
	svc := NewServiceWithClock(newFakeRepo(), nil, fixedClock("2026-03-01"))

	req := validCreate()
	req.Freq, req.ByWeekday = FreqWeekly, []int{4} // Thursday
	req.StartDate = "2026-03-02"                   // Monday
	end := "2026-03-03"                            // window closes before Thursday
	req.EndDate = &end

	_, err := svc.Create(context.Background(), "u1", "p1", "free", req)
	assertCode(t, err, apperror.ErrInvalidRecurrence)
}

// A rule whose series ends after instance #1 stores a null cursor — exhausted,
// not "fires again forever".
func TestCreate_ExhaustedAfterFirstOccurrence(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, nil, fixedClock("2026-03-01"))

	req := validCreate()
	end := "2026-03-05"
	req.EndDate = &end

	view, err := svc.Create(context.Background(), "u1", "p1", "free", req)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if view.NextOccurrence != nil {
		t.Errorf("nextOccurrence = %v, want nil (series exhausted)", *view.NextOccurrence)
	}
}

func TestCreate_EmitsCreatedEvent(t *testing.T) {
	rec := &recorder{}
	svc := NewServiceWithClock(newFakeRepo(), rec, fixedClock("2026-03-01"))

	if _, err := svc.Create(context.Background(), "u1", "p1", "free", validCreate()); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(rec.events) != 1 || rec.events[0].Type != EventCreated {
		t.Fatalf("events = %+v, want one %s", rec.events, EventCreated)
	}
	if _, ok := rec.events[0].Payload.(RuleView); !ok {
		t.Errorf("payload = %T, want a full RuleView", rec.events[0].Payload)
	}
}

// A failed write must not emit — a client would otherwise cache a rule that
// does not exist.
func TestCreate_NoEventOnRepoFailure(t *testing.T) {
	repo := newFakeRepo()
	repo.createErr = errors.New("db down")
	rec := &recorder{}
	svc := NewServiceWithClock(repo, rec, fixedClock("2026-03-01"))

	if _, err := svc.Create(context.Background(), "u1", "p1", "free", validCreate()); err == nil {
		t.Fatal("Create: err = nil, want the repo failure")
	}
	if len(rec.events) != 0 {
		t.Errorf("events = %+v, want none", rec.events)
	}
}

// A nil broadcaster is a valid no-op seam, not a panic.
func TestCreate_NilBroadcasterIsSafe(t *testing.T) {
	svc := NewServiceWithClock(newFakeRepo(), nil, fixedClock("2026-03-01"))
	if _, err := svc.Create(context.Background(), "u1", "p1", "free", validCreate()); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func seedRule(t *testing.T, svc Service) RuleView {
	t.Helper()
	view, err := svc.Create(context.Background(), "u1", "p1", "free", validCreate())
	if err != nil {
		t.Fatalf("seed Create: %v", err)
	}
	return view
}

// Another user's rule is indistinguishable from a missing one — no existence leak.
func TestRowLevelIsolation(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, nil, fixedClock("2026-03-01"))
	view := seedRule(t, svc)

	t.Run("Get", func(t *testing.T) {
		_, err := svc.Get(context.Background(), "u2", view.ID)
		assertCode(t, err, apperror.ErrRecurrenceRuleNotFound)
	})
	t.Run("Update", func(t *testing.T) {
		_, err := svc.Update(context.Background(), "u2", view.ID, "free", UpdateRuleRequest{})
		assertCode(t, err, apperror.ErrRecurrenceRuleNotFound)
	})
	t.Run("SetPaused", func(t *testing.T) {
		_, err := svc.SetPaused(context.Background(), "u2", view.ID, true)
		assertCode(t, err, apperror.ErrRecurrenceRuleNotFound)
	})
	t.Run("Delete", func(t *testing.T) {
		err := svc.Delete(context.Background(), "u2", view.ID)
		assertCode(t, err, apperror.ErrRecurrenceRuleNotFound)
	})
	t.Run("missing id", func(t *testing.T) {
		_, err := svc.Get(context.Background(), "u1", "does-not-exist")
		assertCode(t, err, apperror.ErrRecurrenceRuleNotFound)
	})
}

// A template-only edit leaves the cursor alone: the schedule did not move.
func TestUpdate_TemplateOnlyKeepsCursor(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, nil, fixedClock("2026-03-01"))
	view := seedRule(t, svc)

	title := "Water the ferns"
	got, err := svc.Update(context.Background(), "u1", view.ID, "free", UpdateRuleRequest{Title: &title})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Title != title {
		t.Errorf("title = %q, want %q", got.Title, title)
	}
	if got.NextOccurrence == nil || *got.NextOccurrence != *view.NextOccurrence {
		t.Errorf("nextOccurrence = %v, want it unchanged at %v", got.NextOccurrence, *view.NextOccurrence)
	}
}

// A schedule edit recomputes the cursor so the next fire lands on the new schedule.
func TestUpdate_ScheduleChangeRecomputesCursor(t *testing.T) {
	repo := newFakeRepo()
	// "Today" is Wednesday 2026-03-04; the seeded rule is weekly on Monday.
	svc := NewServiceWithClock(repo, nil, fixedClock("2026-03-04"))
	view := seedRule(t, svc)

	weekdays := []int{4} // move to Thursday
	got, err := svc.Update(context.Background(), "u1", view.ID, "free", UpdateRuleRequest{ByWeekday: &weekdays})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.NextOccurrence == nil || *got.NextOccurrence != "2026-03-05" {
		t.Errorf("nextOccurrence = %v, want 2026-03-05", got.NextOccurrence)
	}
}

// Clearing endDate must un-exhaust a series, which is why EndDate is an
// optional.Field: absent and explicit-null have to differ.
func TestUpdate_ClearingEndDateRevivesSeries(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, nil, fixedClock("2026-03-01"))

	req := validCreate()
	end := "2026-03-05"
	req.EndDate = &end
	view, err := svc.Create(context.Background(), "u1", "p1", "free", req)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if view.NextOccurrence != nil {
		t.Fatalf("precondition: nextOccurrence = %v, want nil", *view.NextOccurrence)
	}

	got, err := svc.Update(context.Background(), "u1", view.ID, "free", UpdateRuleRequest{
		EndDate: optional.Field[string]{Set: true, Value: nil},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.EndDate != nil {
		t.Errorf("endDate = %v, want nil", *got.EndDate)
	}
	if got.NextOccurrence == nil {
		t.Error("nextOccurrence = nil, want the series revived")
	}
}

// Switching frequency drops the constraint that no longer applies, so the stored
// row matches what the engine will read.
func TestUpdate_SwitchingFreqClearsStaleConstraint(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, nil, fixedClock("2026-03-01"))
	view := seedRule(t, svc) // weekly with byWeekday=[1]

	freq := FreqDaily
	got, err := svc.Update(context.Background(), "u1", view.ID, "free", UpdateRuleRequest{Freq: &freq})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(got.ByWeekday) != 0 {
		t.Errorf("byWeekday = %v, want cleared on a daily rule", got.ByWeekday)
	}
}

// Every patchable field round-trips, and an absent field is left alone.
func TestUpdate_PatchesEveryField(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, nil, fixedClock("2026-03-01"))
	view := seedRule(t, svc)

	notes, priority, energy := "water deeply", "high", "deep"
	freq, interval, start := FreqMonthly, 2, "2026-04-05"
	monthday := 5
	got, err := svc.Update(context.Background(), "u1", view.ID, "free", UpdateRuleRequest{
		Notes:            optional.Field[string]{Set: true, Value: &notes},
		Priority:         &priority,
		Energy:           &energy,
		EstimatedMinutes: optional.Field[int]{Set: true, Value: ptrInt(30)},
		Freq:             &freq,
		Interval:         &interval,
		ByMonthday:       optional.Field[int]{Set: true, Value: &monthday},
		StartDate:        &start,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if got.Notes == nil || *got.Notes != notes {
		t.Errorf("notes = %v, want %q", got.Notes, notes)
	}
	if got.Priority != priority || got.Energy != energy {
		t.Errorf("priority/energy = %s/%s, want %s/%s", got.Priority, got.Energy, priority, energy)
	}
	if got.EstimatedMinutes == nil || *got.EstimatedMinutes != 30 {
		t.Errorf("estimatedMinutes = %v, want 30", got.EstimatedMinutes)
	}
	if got.Freq != freq || got.Interval != interval {
		t.Errorf("freq/interval = %s/%d, want %s/%d", got.Freq, got.Interval, freq, interval)
	}
	if got.ByMonthday == nil || *got.ByMonthday != monthday {
		t.Errorf("byMonthday = %v, want %d", got.ByMonthday, monthday)
	}
	if got.StartDate != start {
		t.Errorf("startDate = %s, want %s", got.StartDate, start)
	}
	// The title was never patched.
	if got.Title != view.Title {
		t.Errorf("title = %q, want it unchanged at %q", got.Title, view.Title)
	}
}

// Clearing a nullable field is distinct from leaving it absent.
func TestUpdate_ClearsNullableFields(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, nil, fixedClock("2026-03-01"))

	req := validCreate()
	notes := "original"
	req.Notes, req.EstimatedMinutes = &notes, ptrInt(15)
	view, err := svc.Create(context.Background(), "u1", "p1", "free", req)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := svc.Update(context.Background(), "u1", view.ID, "free", UpdateRuleRequest{
		Notes:            optional.Field[string]{Set: true, Value: nil},
		EstimatedMinutes: optional.Field[int]{Set: true, Value: nil},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Notes != nil {
		t.Errorf("notes = %v, want nil", *got.Notes)
	}
	if got.EstimatedMinutes != nil {
		t.Errorf("estimatedMinutes = %v, want nil", *got.EstimatedMinutes)
	}
}

func TestUpdate_RejectsMalformedDates(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, nil, fixedClock("2026-03-01"))
	view := seedRule(t, svc)

	t.Run("startDate", func(t *testing.T) {
		bad := "not-a-date"
		_, err := svc.Update(context.Background(), "u1", view.ID, "free", UpdateRuleRequest{StartDate: &bad})
		assertCode(t, err, apperror.ErrInvalidDate)
	})
	t.Run("endDate", func(t *testing.T) {
		bad := "2026-13-45"
		_, err := svc.Update(context.Background(), "u1", view.ID, "free", UpdateRuleRequest{
			EndDate: optional.Field[string]{Set: true, Value: &bad},
		})
		assertCode(t, err, apperror.ErrInvalidDate)
	})
}

func TestUpdate_RejectsInvalidSchedule(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, nil, fixedClock("2026-03-01"))
	view := seedRule(t, svc)

	interval := 999
	_, err := svc.Update(context.Background(), "u1", view.ID, "free", UpdateRuleRequest{Interval: &interval})
	assertCode(t, err, apperror.ErrInvalidRecurrence)
}

func TestSetPaused(t *testing.T) {
	repo := newFakeRepo()
	rec := &recorder{}
	svc := NewServiceWithClock(repo, rec, fixedClock("2026-03-01"))
	view := seedRule(t, svc)
	rec.events = nil

	got, err := svc.SetPaused(context.Background(), "u1", view.ID, true)
	if err != nil {
		t.Fatalf("SetPaused: %v", err)
	}
	if !got.Paused {
		t.Error("paused = false, want true")
	}
	if len(rec.events) != 1 || rec.events[0].Type != EventUpdated {
		t.Errorf("events = %+v, want one %s", rec.events, EventUpdated)
	}

	resumed, err := svc.SetPaused(context.Background(), "u1", view.ID, false)
	if err != nil {
		t.Fatalf("SetPaused resume: %v", err)
	}
	if resumed.Paused {
		t.Error("paused = true, want false after resume")
	}
}

func TestDelete_EmitsRefPayload(t *testing.T) {
	repo := newFakeRepo()
	rec := &recorder{}
	svc := NewServiceWithClock(repo, rec, fixedClock("2026-03-01"))
	view := seedRule(t, svc)
	rec.events = nil

	if err := svc.Delete(context.Background(), "u1", view.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if repo.deletedID != view.ID {
		t.Errorf("deleted id = %q, want %q", repo.deletedID, view.ID)
	}
	if len(rec.events) != 1 || rec.events[0].Type != EventDeleted {
		t.Fatalf("events = %+v, want one %s", rec.events, EventDeleted)
	}
	ref, ok := rec.events[0].Payload.(Ref)
	if !ok || ref.ID != view.ID {
		t.Errorf("payload = %+v, want Ref{ID: %s}", rec.events[0].Payload, view.ID)
	}
}

func TestList_FiltersByProject(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, nil, fixedClock("2026-03-01"))

	if _, err := svc.Create(context.Background(), "u1", "p1", "pro", validCreate()); err != nil {
		t.Fatalf("Create p1: %v", err)
	}
	if _, err := svc.Create(context.Background(), "u1", "p2", "pro", validCreate()); err != nil {
		t.Fatalf("Create p2: %v", err)
	}

	all, err := svc.List(context.Background(), "u1", nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all.Items) != 2 {
		t.Errorf("unfiltered items = %d, want 2", len(all.Items))
	}

	p1 := "p1"
	filtered, err := svc.List(context.Background(), "u1", &p1)
	if err != nil {
		t.Fatalf("List filtered: %v", err)
	}
	if len(filtered.Items) != 1 || filtered.Items[0].ProjectID != "p1" {
		t.Errorf("filtered items = %+v, want only p1", filtered.Items)
	}
}

// The wire shape carries dates as YYYY-MM-DD and never a null weekday array.
func TestToView_WireShape(t *testing.T) {
	r := Rule{
		ID: "r1", ProjectID: "p1", Title: "t", Priority: "medium", Energy: "medium",
		Freq: FreqDaily, Interval: 1, StartDate: date("2026-03-01"),
		EndDate: ptrTime("2026-04-01"), NextOccurrence: ptrTime("2026-03-02"),
	}
	v := ToView(r)

	if v.StartDate != "2026-03-01" {
		t.Errorf("startDate = %q, want 2026-03-01", v.StartDate)
	}
	if v.EndDate == nil || *v.EndDate != "2026-04-01" {
		t.Errorf("endDate = %v, want 2026-04-01", v.EndDate)
	}
	if v.NextOccurrence == nil || *v.NextOccurrence != "2026-03-02" {
		t.Errorf("nextOccurrence = %v, want 2026-03-02", v.NextOccurrence)
	}
	if v.ByWeekday == nil {
		t.Error("byWeekday = nil, want an empty array so the client can render it directly")
	}
}
