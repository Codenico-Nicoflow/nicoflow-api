package recurrence

import (
	"context"
	"testing"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/optional"
)

func ptrStr(s string) *string { return &s }

func timedCreate(t string) CreateRuleRequest {
	req := validCreate()
	req.ScheduledTime = ptrStr(t)
	return req
}

func TestCreate_StampsScheduledTimeOnFirstOccurrence(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, &recorder{}, fixedClock("2026-03-01"))

	view, err := svc.Create(context.Background(), "u1", "p1", "pro", timedCreate("09:00"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if view.ScheduledTime == nil || *view.ScheduledTime != "09:00" {
		t.Errorf("view.scheduledTime = %v, want 09:00", view.ScheduledTime)
	}
	if repo.createdOcc.ScheduledTime == nil || *repo.createdOcc.ScheduledTime != "09:00" {
		t.Errorf("occurrence.ScheduledTime = %v, want 09:00", repo.createdOcc.ScheduledTime)
	}
}

func TestCreate_ScheduledTimeIsProOnly(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, &recorder{}, fixedClock("2026-03-01"))

	_, err := svc.Create(context.Background(), "u1", "p1", "free", timedCreate("09:00"))
	assertCode(t, err, apperror.ErrPlanLimitExceeded)
}

func TestCreate_AllDayRuleIsFreeOnEveryPlan(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, &recorder{}, fixedClock("2026-03-01"))

	view, err := svc.Create(context.Background(), "u1", "p1", "free", validCreate())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if view.ScheduledTime != nil {
		t.Errorf("scheduledTime = %v, want nil for an all-day rule", view.ScheduledTime)
	}
}

func TestCreate_RejectsMalformedAndUnsnappedTimes(t *testing.T) {
	tests := []struct {
		name string
		time string
	}{
		{"not a time", "nine o'clock"},
		{"seconds included", "09:00:00"},
		{"off the 15-minute grid", "09:07"},
		{"hour out of range", "25:00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewServiceWithClock(newFakeRepo(), &recorder{}, fixedClock("2026-03-01"))
			_, err := svc.Create(context.Background(), "u1", "p1", "pro", timedCreate(tt.time))
			assertCode(t, err, apperror.ErrInvalidInput)
		})
	}
}

// A timed occurrence must not run past midnight — the task domain assumes a task
// lives on exactly one scheduled_for.
func TestCreate_ClampsEstimateToDayEnd(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, &recorder{}, fixedClock("2026-03-01"))

	req := timedCreate("23:00")
	req.EstimatedMinutes = ptrInt(120)
	view, err := svc.Create(context.Background(), "u1", "p1", "pro", req)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if view.EstimatedMinutes == nil || *view.EstimatedMinutes != 59 {
		t.Errorf("estimatedMinutes = %v, want 59 (23:00 → 23:59)", view.EstimatedMinutes)
	}
}

func TestUpdate_SetsAndClearsScheduledTime(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, &recorder{}, fixedClock("2026-03-01"))
	view, err := svc.Create(context.Background(), "u1", "p1", "pro", timedCreate("09:00"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	moved, err := svc.Update(context.Background(), "u1", view.ID, "pro", UpdateRuleRequest{
		ScheduledTime: optional.Field[string]{Set: true, Value: ptrStr("18:30")},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if moved.ScheduledTime == nil || *moved.ScheduledTime != "18:30" {
		t.Errorf("scheduledTime = %v, want 18:30", moved.ScheduledTime)
	}

	// An explicit null clears the time — allowed on every plan, so a downgraded
	// user is never trapped holding a rule they cannot edit.
	cleared, err := svc.Update(context.Background(), "u1", view.ID, "free", UpdateRuleRequest{
		ScheduledTime: optional.Field[string]{Set: true, Value: nil},
	})
	if err != nil {
		t.Fatalf("Update() clear error = %v", err)
	}
	if cleared.ScheduledTime != nil {
		t.Errorf("scheduledTime = %v, want nil after an explicit null", cleared.ScheduledTime)
	}
}

func TestUpdate_SettingScheduledTimeIsProOnly(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, &recorder{}, fixedClock("2026-03-01"))
	view, err := svc.Create(context.Background(), "u1", "p1", "free", validCreate())
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err = svc.Update(context.Background(), "u1", view.ID, "free", UpdateRuleRequest{
		ScheduledTime: optional.Field[string]{Set: true, Value: ptrStr("09:00")},
	})
	assertCode(t, err, apperror.ErrPlanLimitExceeded)
}

// An absent scheduledTime must leave the stored one alone, so a free user
// editing a title on a rule they set up while Pro is not blocked.
func TestUpdate_AbsentScheduledTimeIsUntouchedOnFreePlan(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, &recorder{}, fixedClock("2026-03-01"))
	view, err := svc.Create(context.Background(), "u1", "p1", "pro", timedCreate("09:00"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	title := "Renamed"
	got, err := svc.Update(context.Background(), "u1", view.ID, "free", UpdateRuleRequest{Title: &title})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if got.ScheduledTime == nil || *got.ScheduledTime != "09:00" {
		t.Errorf("scheduledTime = %v, want the stored 09:00", got.ScheduledTime)
	}
}

// The materializer stamps the rule's time onto every successor, which is the
// whole point of the time living on the rule rather than the occurrence.
func TestMaterializeAfterCompletion_InheritsScheduledTime(t *testing.T) {
	repo := newFakeRepo()
	svc := NewServiceWithClock(repo, &recorder{}, fixedClock("2026-03-01"))
	view, err := svc.Create(context.Background(), "u1", "p1", "pro", timedCreate("09:00"))
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	spy := &materializeSpy{fakeRepo: repo}
	m := NewMaterializerWithClock(spy, &recorder{}, nil, 50, fixedClock("2026-03-02"))
	if err := m.MaterializeAfterCompletion(context.Background(), "u1", view.ID); err != nil {
		t.Fatalf("MaterializeAfterCompletion() error = %v", err)
	}
	if spy.got.ScheduledTime == nil || *spy.got.ScheduledTime != "09:00" {
		t.Errorf("successor ScheduledTime = %v, want 09:00", spy.got.ScheduledTime)
	}
}

// materializeSpy captures the occurrence handed to Materialize.
type materializeSpy struct {
	*fakeRepo
	got Occurrence
}

func (s *materializeSpy) Materialize(_ context.Context, _ Rule, occ Occurrence, _ int) (MaterializeResult, error) {
	s.got = occ
	return MaterializeResult{Created: &occ}, nil
}
