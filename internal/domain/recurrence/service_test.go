package recurrence

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/optional"
)

// fakeRepo records what the service asked for and returns scripted results.
type fakeRepo struct {
	rules        map[string]Rule
	ruleOrder    []string // insertion order, for IsWithinFreeLimit's ranking
	count        int
	projectOwned bool

	createdOcc   Occurrence
	deletedID    string
	createErr    error
	statusCounts map[string]int
	statuses     []string

	// ConvertTask scripting: which task ids already have a rule (so a second
	// convert attempt on them 409s, mirroring the real UPDATE ... WHERE
	// recurrence_rule_id IS NULL guard), and which id was last converted.
	alreadyRecurring map[string]bool
	convertedTaskID  string
	convertOccDate   time.Time
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{rules: map[string]Rule{}, projectOwned: true, alreadyRecurring: map[string]bool{}}
}

// fakeTaskReader scripts ConvertToRecurring's TaskTemplateReader dependency —
// the task template a conversion reads instead of trusting the client's copy.
type fakeTaskReader struct {
	templates map[string]TaskTemplate
	err       error
}

func newFakeTaskReader() *fakeTaskReader {
	return &fakeTaskReader{templates: map[string]TaskTemplate{}}
}

func (f *fakeTaskReader) GetTemplate(_ context.Context, userID, taskID string) (TaskTemplate, error) {
	if f.err != nil {
		return TaskTemplate{}, f.err
	}
	tmpl, ok := f.templates[taskID]
	if !ok {
		return TaskTemplate{}, apperror.New(http.StatusNotFound, apperror.ErrTaskNotFound, "task not found")
	}
	_ = userID // row-scoping is the real adapter's job; the fake trusts the map key
	return tmpl, nil
}

// seedRuleCount pre-populates n placeholder rules so CreateWithOccurrence's
// len(f.rules) guard (and IsWithinFreeLimit's ranking) see them, mirroring
// what a real user's existing rule count would look like.
func (f *fakeRepo) seedRuleCount(n int) {
	for i := range n {
		id := fmt.Sprintf("seed-%d", i)
		f.rules[id] = Rule{ID: id, UserID: "u1"}
		f.ruleOrder = append(f.ruleOrder, id)
	}
}

func (f *fakeRepo) CreateWithOccurrence(_ context.Context, r Rule, occ Occurrence, freeLimit int) (Rule, error) {
	if f.createErr != nil {
		return Rule{}, f.createErr
	}
	if freeLimit > 0 && len(f.rules) >= freeLimit {
		return Rule{}, apperror.New(http.StatusForbidden, apperror.ErrPlanLimitExceeded, "free plan allows up to 3 recurrence rules")
	}
	f.createdOcc = occ
	r.CreatedAt, r.UpdatedAt = time.Now(), time.Now()
	f.rules[r.ID] = r
	f.ruleOrder = append(f.ruleOrder, r.ID)
	return r, nil
}

func (f *fakeRepo) ConvertTask(_ context.Context, taskID string, r Rule, occDate time.Time, freeLimit int) (Rule, error) {
	if freeLimit > 0 && len(f.rules) >= freeLimit {
		return Rule{}, apperror.New(http.StatusForbidden, apperror.ErrPlanLimitExceeded, "free plan allows up to 3 recurrence rules")
	}
	if f.alreadyRecurring[taskID] {
		return Rule{}, apperror.New(http.StatusConflict, apperror.ErrTaskAlreadyRecurring, "this task is already recurring")
	}
	r.CreatedAt, r.UpdatedAt = time.Now(), time.Now()
	f.rules[r.ID] = r
	f.ruleOrder = append(f.ruleOrder, r.ID)
	f.convertedTaskID = taskID
	f.convertOccDate = occDate
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

func (f *fakeRepo) IsWithinFreeLimit(_ context.Context, _, ruleID string, limit int) (bool, error) {
	for i, id := range f.ruleOrder {
		if id == ruleID {
			return i < limit, nil
		}
	}
	return false, nil
}

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

func (f *fakeRepo) ReapOverdue(context.Context) ([]Occurrence, error) { return nil, nil }

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
	svc := NewServiceWithClock(repo, rec, newFakeTaskReader(), fixedClock("2026-03-01"))

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
			repo.seedRuleCount(tt.existing) // CreateWithOccurrence's guard reads len(rules), not count
			svc := NewServiceWithClock(repo, nil, newFakeTaskReader(), fixedClock("2026-03-01"))

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
	svc := NewServiceWithClock(repo, nil, newFakeTaskReader(), fixedClock("2026-03-01"))

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
			svc := NewServiceWithClock(newFakeRepo(), nil, newFakeTaskReader(), fixedClock("2026-03-01"))

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
	svc := NewServiceWithClock(repo, nil, newFakeTaskReader(), fixedClock("2026-01-01"))

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
	svc := NewServiceWithClock(newFakeRepo(), nil, newFakeTaskReader(), fixedClock("2026-03-01"))

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
	svc := NewServiceWithClock(repo, nil, newFakeTaskReader(), fixedClock("2026-03-01"))

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
	svc := NewServiceWithClock(newFakeRepo(), rec, newFakeTaskReader(), fixedClock("2026-03-01"))

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
	svc := NewServiceWithClock(repo, rec, newFakeTaskReader(), fixedClock("2026-03-01"))

	if _, err := svc.Create(context.Background(), "u1", "p1", "free", validCreate()); err == nil {
		t.Fatal("Create: err = nil, want the repo failure")
	}
	if len(rec.events) != 0 {
		t.Errorf("events = %+v, want none", rec.events)
	}
}

// A nil broadcaster is a valid no-op seam, not a panic.
func TestCreate_NilBroadcasterIsSafe(t *testing.T) {
	svc := NewServiceWithClock(newFakeRepo(), nil, newFakeTaskReader(), fixedClock("2026-03-01"))
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
	svc := NewServiceWithClock(repo, nil, newFakeTaskReader(), fixedClock("2026-03-01"))
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
		_, err := svc.SetPaused(context.Background(), "u2", view.ID, "free", true)
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
	svc := NewServiceWithClock(repo, nil, newFakeTaskReader(), fixedClock("2026-03-01"))
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
	svc := NewServiceWithClock(repo, nil, newFakeTaskReader(), fixedClock("2026-03-04"))
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
	svc := NewServiceWithClock(repo, nil, newFakeTaskReader(), fixedClock("2026-03-01"))

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
	svc := NewServiceWithClock(repo, nil, newFakeTaskReader(), fixedClock("2026-03-01"))
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
	svc := NewServiceWithClock(repo, nil, newFakeTaskReader(), fixedClock("2026-03-01"))
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
	svc := NewServiceWithClock(repo, nil, newFakeTaskReader(), fixedClock("2026-03-01"))

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
	svc := NewServiceWithClock(repo, nil, newFakeTaskReader(), fixedClock("2026-03-01"))
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
	svc := NewServiceWithClock(repo, nil, newFakeTaskReader(), fixedClock("2026-03-01"))
	view := seedRule(t, svc)

	interval := 999
	_, err := svc.Update(context.Background(), "u1", view.ID, "free", UpdateRuleRequest{Interval: &interval})
	assertCode(t, err, apperror.ErrInvalidRecurrence)
}

// A graceful downgrade keeps the user's oldest freePlanRuleLimit rules
// editable and makes the rest read-only until deleted or they re-upgrade —
// otherwise a Pro user with 10 rules keeps full edit/pause on all 10 forever
// after downgrading, silently bypassing the cap a new free user can't get past.
func TestUpdate_ReadOnlyOverFreeLimitAfterDowngrade(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, nil, newFakeTaskReader(), fixedClock("2026-03-01"))

	views := make([]RuleView, 0, 4)
	for range 4 {
		// Created on "pro" so the 4th rule (over the free cap) can exist at all.
		v, err := svc.Create(context.Background(), "u1", "p1", "pro", validCreate())
		if err != nil {
			t.Fatalf("seed Create: %v", err)
		}
		views = append(views, v)
	}

	title := "renamed"
	if _, err := svc.Update(context.Background(), "u1", views[0].ID, "free", UpdateRuleRequest{Title: &title}); err != nil {
		t.Errorf("Update on rule 1 of 4 (within the free cap) = %v, want success", err)
	}

	_, err := svc.Update(context.Background(), "u1", views[3].ID, "free", UpdateRuleRequest{Title: &title})
	assertCode(t, err, apperror.ErrPlanLimitExceeded)

	// Pro never hits the gate, even on the same over-cap rule.
	if _, err := svc.Update(context.Background(), "u1", views[3].ID, "pro", UpdateRuleRequest{Title: &title}); err != nil {
		t.Errorf("Update on rule 4 of 4 as pro = %v, want success", err)
	}
}

// Resuming an over-cap paused rule is un-pausing a rule the free plan wouldn't
// let the user create today, so it's gated the same as any other edit — but
// pausing one (reducing what fires) is always allowed, even over the cap.
func TestSetPaused_ReadOnlyOverFreeLimitAfterDowngrade(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, nil, newFakeTaskReader(), fixedClock("2026-03-01"))

	views := make([]RuleView, 0, 4)
	for range 4 {
		v, err := svc.Create(context.Background(), "u1", "p1", "pro", validCreate())
		if err != nil {
			t.Fatalf("seed Create: %v", err)
		}
		views = append(views, v)
	}

	if _, err := svc.SetPaused(context.Background(), "u1", views[3].ID, "free", true); err != nil {
		t.Errorf("pausing over-cap rule = %v, want success", err)
	}
	_, err := svc.SetPaused(context.Background(), "u1", views[3].ID, "free", false)
	assertCode(t, err, apperror.ErrPlanLimitExceeded)
}

func TestSetPaused(t *testing.T) {
	repo := newFakeRepo()
	rec := &recorder{}
	svc := NewServiceWithClock(repo, rec, newFakeTaskReader(), fixedClock("2026-03-01"))
	view := seedRule(t, svc)
	rec.events = nil

	got, err := svc.SetPaused(context.Background(), "u1", view.ID, "free", true)
	if err != nil {
		t.Fatalf("SetPaused: %v", err)
	}
	if !got.Paused {
		t.Error("paused = false, want true")
	}
	if len(rec.events) != 1 || rec.events[0].Type != EventUpdated {
		t.Errorf("events = %+v, want one %s", rec.events, EventUpdated)
	}

	resumed, err := svc.SetPaused(context.Background(), "u1", view.ID, "free", false)
	if err != nil {
		t.Fatalf("SetPaused resume: %v", err)
	}
	if resumed.Paused {
		t.Error("paused = true, want false after resume")
	}
}

func plainTaskTemplate() TaskTemplate {
	return TaskTemplate{
		ProjectID: "p1",
		Title:     "Wash the floors",
		Priority:  "high",
		Energy:    "low",
	}
}

// The request's own template fields must be ignored — the task's stored
// values are what the rule is built from, never a client-supplied copy that
// could drift from what's actually on the row.
func TestConvertToRecurring_UsesTaskTemplateNotRequestTemplate(t *testing.T) {
	repo := newFakeRepo()
	reader := newFakeTaskReader()
	reader.templates["t1"] = plainTaskTemplate()
	svc := NewServiceWithClock(repo, nil, reader, fixedClock("2026-03-01"))

	req := validCreate()
	req.Title = "ignored client title"
	req.Priority = "low"
	req.Energy = "deep"

	view, err := svc.ConvertToRecurring(context.Background(), "u1", "t1", "free", req)
	if err != nil {
		t.Fatalf("ConvertToRecurring: %v", err)
	}
	if view.Title != "Wash the floors" || view.Priority != "high" || view.Energy != "low" {
		t.Errorf("rule = %+v, want the task's own template, not the request's", view)
	}
	if repo.convertedTaskID != "t1" {
		t.Errorf("convertedTaskID = %q, want t1", repo.convertedTaskID)
	}
}

// A task that already belongs to a rule must be rejected — re-parenting it
// silently would orphan the old rule's cursor/history unexpectedly. Editing
// the existing rule is the only supported path once a task is recurring.
func TestConvertToRecurring_RejectsAlreadyRecurringTask(t *testing.T) {
	repo := newFakeRepo()
	repo.alreadyRecurring["t1"] = true
	reader := newFakeTaskReader()
	reader.templates["t1"] = plainTaskTemplate()
	svc := NewServiceWithClock(repo, nil, reader, fixedClock("2026-03-01"))

	_, err := svc.ConvertToRecurring(context.Background(), "u1", "t1", "free", validCreate())
	assertCode(t, err, apperror.ErrTaskAlreadyRecurring)
}

// A missing/foreign task 404s exactly like every other task-scoped endpoint —
// the reader's row-scoping is what a real caller relies on for isolation.
func TestConvertToRecurring_TaskNotFound(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, nil, newFakeTaskReader(), fixedClock("2026-03-01"))

	_, err := svc.ConvertToRecurring(context.Background(), "u1", "missing", "free", validCreate())
	assertCode(t, err, apperror.ErrTaskNotFound)
}

// Convert goes through the identical free-plan gating as create — a rule is a
// rule regardless of whether instance #1 came from an existing task.
func TestConvertToRecurring_RespectsFreePlanCap(t *testing.T) {
	repo := newFakeRepo()
	repo.seedRuleCount(3)
	reader := newFakeTaskReader()
	reader.templates["t1"] = plainTaskTemplate()
	svc := NewServiceWithClock(repo, nil, reader, fixedClock("2026-03-01"))

	_, err := svc.ConvertToRecurring(context.Background(), "u1", "t1", "free", validCreate())
	assertCode(t, err, apperror.ErrPlanLimitExceeded)
}

// Setting a scheduledTime on convert is gated exactly like create — Pro-only.
func TestConvertToRecurring_ScheduledTimeIsProOnly(t *testing.T) {
	repo := newFakeRepo()
	reader := newFakeTaskReader()
	reader.templates["t1"] = plainTaskTemplate()
	svc := NewServiceWithClock(repo, nil, reader, fixedClock("2026-03-01"))

	req := timedCreate("09:00")
	_, err := svc.ConvertToRecurring(context.Background(), "u1", "t1", "free", req)
	assertCode(t, err, apperror.ErrPlanLimitExceeded)
}

// The projectId on convert always comes from the task's own row — never the
// request — so a client can't move the resulting rule into a project it
// doesn't actually belong to.
func TestConvertToRecurring_ProjectIDComesFromTask(t *testing.T) {
	repo := newFakeRepo()
	reader := newFakeTaskReader()
	tmpl := plainTaskTemplate()
	tmpl.ProjectID = "p-real"
	reader.templates["t1"] = tmpl
	svc := NewServiceWithClock(repo, nil, reader, fixedClock("2026-03-01"))

	req := validCreate()
	view, err := svc.ConvertToRecurring(context.Background(), "u1", "t1", "free", req)
	if err != nil {
		t.Fatalf("ConvertToRecurring: %v", err)
	}
	if view.ProjectID != "p-real" {
		t.Errorf("projectId = %q, want p-real (from the task, not the request)", view.ProjectID)
	}
}

// Emits recurrence.created (unchanged event/payload) — the frontend's existing
// TASK_TAGS invalidation on that event already covers "some task changed", so
// convert deliberately doesn't need a new event type.
func TestConvertToRecurring_EmitsCreatedEvent(t *testing.T) {
	repo := newFakeRepo()
	rec := &recorder{}
	reader := newFakeTaskReader()
	reader.templates["t1"] = plainTaskTemplate()
	svc := NewServiceWithClock(repo, rec, reader, fixedClock("2026-03-01"))

	if _, err := svc.ConvertToRecurring(context.Background(), "u1", "t1", "free", validCreate()); err != nil {
		t.Fatalf("ConvertToRecurring: %v", err)
	}
	if len(rec.events) != 1 || rec.events[0].Type != EventCreated {
		t.Errorf("events = %+v, want one %s", rec.events, EventCreated)
	}
}

func TestDelete_EmitsRefPayload(t *testing.T) {
	repo := newFakeRepo()
	rec := &recorder{}
	svc := NewServiceWithClock(repo, rec, newFakeTaskReader(), fixedClock("2026-03-01"))
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
	svc := NewServiceWithClock(repo, nil, newFakeTaskReader(), fixedClock("2026-03-01"))

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
