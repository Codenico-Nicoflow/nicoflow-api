package habit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
)

// ── Test doubles ─────────────────────────────────────────────────────────────

type mockRepo struct {
	list        func(ctx context.Context, userID string, includeArchived bool) ([]Habit, error)
	getByID     func(ctx context.Context, userID, id string) (Habit, error)
	create      func(ctx context.Context, h Habit) (Habit, error)
	update      func(ctx context.Context, p UpdateParams) (Habit, bool, error)
	archive     func(ctx context.Context, userID, id string) (bool, error)
	countActive func(ctx context.Context, userID string) (int, error)
}

func (m *mockRepo) List(ctx context.Context, userID string, includeArchived bool) ([]Habit, error) {
	if m.list != nil {
		return m.list(ctx, userID, includeArchived)
	}
	return nil, nil
}

func (m *mockRepo) GetByID(ctx context.Context, userID, id string) (Habit, error) {
	if m.getByID != nil {
		return m.getByID(ctx, userID, id)
	}
	return Habit{}, notFound()
}

func (m *mockRepo) Create(ctx context.Context, h Habit) (Habit, error) {
	if m.create != nil {
		return m.create(ctx, h)
	}
	return h, nil
}

func (m *mockRepo) Update(ctx context.Context, p UpdateParams) (Habit, bool, error) {
	if m.update != nil {
		return m.update(ctx, p)
	}
	return Habit{ID: p.ID, UserID: p.UserID, Name: p.Name, ScheduleKind: p.ScheduleKind}, true, nil
}

func (m *mockRepo) Archive(ctx context.Context, userID, id string) (bool, error) {
	if m.archive != nil {
		return m.archive(ctx, userID, id)
	}
	return true, nil
}

func (m *mockRepo) CountActive(ctx context.Context, userID string) (int, error) {
	if m.countActive != nil {
		return m.countActive(ctx, userID)
	}
	return 0, nil
}

type recordingBroadcaster struct{ events []Event }

func (b *recordingBroadcaster) Broadcast(_ string, ev Event) { b.events = append(b.events, ev) }

// errCode extracts the typed apperror code, or "" when err is not an AppError.
// Tests assert on the code string, never on the HTTP status alone.
func errCode(err error) string {
	if ae, ok := errors.AsType[*apperror.AppError](err); ok {
		return ae.Code
	}
	return ""
}

func intPtr(v int) *int       { return &v }
func i16Ptr(v int16) *int16   { return &v }
func strPtr(v string) *string { return &v }
func boolPtr(v bool) *bool    { return &v }

// ── Create ───────────────────────────────────────────────────────────────────

// AC1: a valid create returns a habit carrying a string id.
func TestCreate_HappyPath(t *testing.T) {
	svc := NewService(&mockRepo{}, nil)

	view, err := svc.Create(context.Background(), "u1", "free", CreateHabitRequest{
		Name: "  Read  ", ScheduleKind: ScheduleDaily,
		TargetValue: intPtr(20), Unit: strPtr("minutes"),
	})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	if view.ID == "" {
		t.Error("id is empty, want a generated string id")
	}
	if view.Name != "Read" {
		t.Errorf("name = %q, want %q (trimmed)", view.Name, "Read")
	}
	if view.Subject != DefaultSubject || view.Color != DefaultColor {
		t.Errorf("subject/color = %q/%q, want defaults %q/%q",
			view.Subject, view.Color, DefaultSubject, DefaultColor)
	}
	if view.Polarity != PolarityBuild {
		t.Errorf("polarity = %q, want %q by default", view.Polarity, PolarityBuild)
	}
}

func TestCreate_Validation(t *testing.T) {
	tests := []struct {
		name string
		req  CreateHabitRequest
		want string
	}{
		{
			name: "empty name",
			req:  CreateHabitRequest{Name: "   "},
			want: apperror.ErrInvalidInput,
		},
		{
			name: "unknown polarity",
			req:  CreateHabitRequest{Name: "Read", Polarity: "maybe"},
			want: apperror.ErrInvalidInput,
		},
		{
			name: "unknown schedule kind",
			req:  CreateHabitRequest{Name: "Read", ScheduleKind: "fortnightly"},
			want: apperror.ErrInvalidInput,
		},
		{
			name: "weekdays without days",
			req:  CreateHabitRequest{Name: "Gym", ScheduleKind: ScheduleWeekdays},
			want: apperror.ErrInvalidInput,
		},
		{
			name: "weekday out of range",
			req:  CreateHabitRequest{Name: "Gym", ScheduleKind: ScheduleWeekdays, ByWeekday: []int16{7}},
			want: apperror.ErrInvalidInput,
		},
		{
			name: "quota without count",
			req:  CreateHabitRequest{Name: "Read", ScheduleKind: ScheduleWeeklyQuota},
			want: apperror.ErrInvalidInput,
		},
		{
			name: "quota out of range",
			req:  CreateHabitRequest{Name: "Read", ScheduleKind: ScheduleWeeklyQuota, TimesPerWeek: i16Ptr(8)},
			want: apperror.ErrInvalidInput,
		},
		{
			name: "negative target",
			req:  CreateHabitRequest{Name: "Read", TargetValue: intPtr(-1)},
			want: apperror.ErrInvalidInput,
		},
		{
			// A build habit with target 0 is satisfied by doing nothing.
			name: "build habit with zero target",
			req:  CreateHabitRequest{Name: "Read", TargetValue: intPtr(0)},
			want: apperror.ErrInvalidInput,
		},
	}

	svc := NewService(&mockRepo{}, nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.Create(context.Background(), "u1", "free", tt.req)
			if got := errCode(err); got != tt.want {
				t.Errorf("error code = %q, want %q (err=%v)", got, tt.want, err)
			}
		})
	}
}

// A quit habit may target zero — that is the whole point of quitting.
func TestCreate_QuitHabitAllowsZeroTarget(t *testing.T) {
	svc := NewService(&mockRepo{}, nil)

	view, err := svc.Create(context.Background(), "u1", "pro", CreateHabitRequest{
		Name: "No drinking", Polarity: PolarityQuit, TargetValue: intPtr(0),
	})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	if view.TargetValue != 0 || view.Polarity != PolarityQuit {
		t.Errorf("got target=%d polarity=%q, want 0/%q", view.TargetValue, view.Polarity, PolarityQuit)
	}
}

func TestCreate_WeekdaysDeduplicated(t *testing.T) {
	var got Habit
	repo := &mockRepo{create: func(_ context.Context, h Habit) (Habit, error) { got = h; return h, nil }}

	_, err := NewService(repo, nil).Create(context.Background(), "u1", "pro", CreateHabitRequest{
		Name: "Gym", ScheduleKind: ScheduleWeekdays, ByWeekday: []int16{1, 3, 1, 5, 3},
	})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	if len(got.ByWeekday) != 3 {
		t.Errorf("byWeekday = %v, want 3 unique days", got.ByWeekday)
	}
}

// Only the fields the chosen schedule kind needs are persisted — a leftover
// byWeekday on a quota habit would describe two different schedules at once.
func TestCreate_ClearsIrrelevantScheduleFields(t *testing.T) {
	var got Habit
	repo := &mockRepo{create: func(_ context.Context, h Habit) (Habit, error) { got = h; return h, nil }}

	_, err := NewService(repo, nil).Create(context.Background(), "u1", "pro", CreateHabitRequest{
		Name: "Read", ScheduleKind: ScheduleWeeklyQuota,
		TimesPerWeek: i16Ptr(3), ByWeekday: []int16{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	if got.ByWeekday != nil {
		t.Errorf("byWeekday = %v, want nil on a weekly_quota habit", got.ByWeekday)
	}
	if got.TimesPerWeek == nil || *got.TimesPerWeek != 3 {
		t.Errorf("timesPerWeek = %v, want 3", got.TimesPerWeek)
	}
}

// ── Plan limits ──────────────────────────────────────────────────────────────

// AC2: the 4th active habit on free is refused and nothing is inserted.
func TestCreate_PlanLimit(t *testing.T) {
	tests := []struct {
		name        string
		plan        string
		activeCount int
		wantCode    string
	}{
		{name: "free under the limit", plan: "free", activeCount: 2, wantCode: ""},
		{name: "free at the limit", plan: "free", activeCount: FreePlanHabitLimit, wantCode: apperror.ErrPlanLimitExceeded},
		{name: "free over the limit after downgrade", plan: "free", activeCount: 9, wantCode: apperror.ErrPlanLimitExceeded},
		{name: "pro at the free limit", plan: "pro", activeCount: FreePlanHabitLimit, wantCode: ""},
		{name: "pro far over the free limit", plan: "pro", activeCount: 50, wantCode: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			created := false
			repo := &mockRepo{
				countActive: func(context.Context, string) (int, error) { return tt.activeCount, nil },
				create: func(_ context.Context, h Habit) (Habit, error) {
					created = true
					return h, nil
				},
			}

			_, err := NewService(repo, nil).Create(context.Background(), "u1", tt.plan,
				CreateHabitRequest{Name: "Read"})

			if got := errCode(err); got != tt.wantCode {
				t.Fatalf("error code = %q, want %q (err=%v)", got, tt.wantCode, err)
			}
			if wantCreated := tt.wantCode == ""; created != wantCreated {
				t.Errorf("repo.Create called = %v, want %v", created, wantCreated)
			}
		})
	}
}

// A pro user does not pay for a COUNT(*) they can never fail.
func TestCreate_ProSkipsTheCount(t *testing.T) {
	counted := false
	repo := &mockRepo{countActive: func(context.Context, string) (int, error) {
		counted = true
		return 0, nil
	}}

	if _, err := NewService(repo, nil).Create(context.Background(), "u1", "pro",
		CreateHabitRequest{Name: "Read"}); err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
	if counted {
		t.Error("CountActive was called for a pro user, want the check skipped")
	}
}

// AC3: a downgraded user keeps editing every habit — streaks are never frozen
// on downgrade — but cannot create another.
func TestUpdate_DowngradedUserCanStillEdit(t *testing.T) {
	repo := &mockRepo{
		getByID: func(context.Context, string, string) (Habit, error) {
			return Habit{ID: "h1", UserID: "u1", Name: "Read", ScheduleKind: ScheduleDaily, TargetValue: 1}, nil
		},
		countActive: func(context.Context, string) (int, error) { return 9, nil },
	}

	if _, err := NewService(repo, nil).Update(context.Background(), "u1", "free", "h1",
		UpdateHabitRequest{Name: strPtr("Read more")}); err != nil {
		t.Fatalf("Update() error = %v, want nil for a downgraded user", err)
	}
}

// AC4: archiving frees a slot, and un-archiving over the limit is refused
// exactly like a create.
func TestUpdate_RestoreIsPlanGated(t *testing.T) {
	tests := []struct {
		name        string
		plan        string
		archived    bool
		activeCount int
		wantCode    string
	}{
		{name: "restore under the limit", plan: "free", archived: true, activeCount: 2, wantCode: ""},
		{name: "restore at the limit", plan: "free", archived: true, activeCount: FreePlanHabitLimit, wantCode: apperror.ErrPlanLimitExceeded},
		{name: "restore at the limit on pro", plan: "pro", archived: true, activeCount: 99, wantCode: ""},
		{name: "editing an active habit is never gated", plan: "free", archived: false, activeCount: 9, wantCode: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			archivedAt := time.Now().UTC()
			cur := Habit{ID: "h1", UserID: "u1", Name: "Read", ScheduleKind: ScheduleDaily, TargetValue: 1}
			if tt.archived {
				cur.ArchivedAt = &archivedAt
			}

			repo := &mockRepo{
				getByID:     func(context.Context, string, string) (Habit, error) { return cur, nil },
				countActive: func(context.Context, string) (int, error) { return tt.activeCount, nil },
			}

			_, err := NewService(repo, nil).Update(context.Background(), "u1", tt.plan, "h1",
				UpdateHabitRequest{Archived: boolPtr(false)})

			if got := errCode(err); got != tt.wantCode {
				t.Errorf("error code = %q, want %q (err=%v)", got, tt.wantCode, err)
			}
		})
	}
}

// ── Update ───────────────────────────────────────────────────────────────────

// Switching schedule kind must not carry the previous kind's fields across.
func TestUpdate_SwitchingScheduleKindClearsStaleFields(t *testing.T) {
	cur := Habit{
		ID: "h1", UserID: "u1", Name: "Gym", TargetValue: 1,
		ScheduleKind: ScheduleWeekdays, ByWeekday: []int16{1, 3, 5},
	}
	var got UpdateParams
	repo := &mockRepo{
		getByID: func(context.Context, string, string) (Habit, error) { return cur, nil },
		update: func(_ context.Context, p UpdateParams) (Habit, bool, error) {
			got = p
			return Habit{ID: p.ID, ScheduleKind: p.ScheduleKind}, true, nil
		},
	}

	_, err := NewService(repo, nil).Update(context.Background(), "u1", "pro", "h1", UpdateHabitRequest{
		ScheduleKind: strPtr(ScheduleWeeklyQuota), TimesPerWeek: i16Ptr(3),
	})
	if err != nil {
		t.Fatalf("Update() error = %v, want nil", err)
	}
	if got.ByWeekday != nil {
		t.Errorf("byWeekday = %v, want nil after switching to weekly_quota", got.ByWeekday)
	}
	if got.ScheduleChangedAt == nil {
		t.Error("scheduleChangedAt is nil, want it stamped so past periods keep their shape")
	}
}

// A cosmetic edit is not a schedule change; stamping the marker would wrongly
// re-shape history.
func TestUpdate_CosmeticEditDoesNotStampScheduleChange(t *testing.T) {
	cur := Habit{ID: "h1", UserID: "u1", Name: "Read", ScheduleKind: ScheduleDaily, TargetValue: 1}
	var got UpdateParams
	repo := &mockRepo{
		getByID: func(context.Context, string, string) (Habit, error) { return cur, nil },
		update: func(_ context.Context, p UpdateParams) (Habit, bool, error) {
			got = p
			return cur, true, nil
		},
	}

	_, err := NewService(repo, nil).Update(context.Background(), "u1", "pro", "h1",
		UpdateHabitRequest{Name: strPtr("Read daily"), Color: strPtr("emerald")})
	if err != nil {
		t.Fatalf("Update() error = %v, want nil", err)
	}
	if got.ScheduleChangedAt != nil {
		t.Error("scheduleChangedAt was stamped for a name/colour edit, want nil")
	}
}

// Re-sending the same weekdays is not a change either.
func TestUpdate_IdenticalScheduleDoesNotStampScheduleChange(t *testing.T) {
	cur := Habit{
		ID: "h1", UserID: "u1", Name: "Gym", TargetValue: 1,
		ScheduleKind: ScheduleWeekdays, ByWeekday: []int16{1, 3, 5},
	}
	var got UpdateParams
	repo := &mockRepo{
		getByID: func(context.Context, string, string) (Habit, error) { return cur, nil },
		update: func(_ context.Context, p UpdateParams) (Habit, bool, error) {
			got = p
			return cur, true, nil
		},
	}

	_, err := NewService(repo, nil).Update(context.Background(), "u1", "pro", "h1",
		UpdateHabitRequest{ByWeekday: []int16{1, 3, 5}})
	if err != nil {
		t.Fatalf("Update() error = %v, want nil", err)
	}
	if got.ScheduleChangedAt != nil {
		t.Error("scheduleChangedAt was stamped for an unchanged schedule, want nil")
	}
}

// The target rule follows the stored polarity, which the request cannot change.
func TestUpdate_ZeroTargetRejectedOnBuildHabit(t *testing.T) {
	repo := &mockRepo{getByID: func(context.Context, string, string) (Habit, error) {
		return Habit{ID: "h1", UserID: "u1", Name: "Read", Polarity: PolarityBuild,
			ScheduleKind: ScheduleDaily, TargetValue: 20}, nil
	}}

	_, err := NewService(repo, nil).Update(context.Background(), "u1", "pro", "h1",
		UpdateHabitRequest{TargetValue: intPtr(0)})
	if got := errCode(err); got != apperror.ErrInvalidInput {
		t.Errorf("error code = %q, want %q", got, apperror.ErrInvalidInput)
	}
}

// AC6: a habit that does not exist, or belongs to someone else, is reported
// identically — the repository fake returns the same not-found either way.
func TestUpdate_MissingHabit(t *testing.T) {
	repo := &mockRepo{getByID: func(context.Context, string, string) (Habit, error) {
		return Habit{}, notFound()
	}}

	_, err := NewService(repo, nil).Update(context.Background(), "u1", "pro", "nope",
		UpdateHabitRequest{Name: strPtr("x")})
	if got := errCode(err); got != apperror.ErrHabitNotFound {
		t.Errorf("error code = %q, want %q", got, apperror.ErrHabitNotFound)
	}
}

// A row that vanishes between the read and the write is a miss, not a 500.
func TestUpdate_RowDisappearsMidFlight(t *testing.T) {
	repo := &mockRepo{
		getByID: func(context.Context, string, string) (Habit, error) {
			return Habit{ID: "h1", UserID: "u1", Name: "Read", ScheduleKind: ScheduleDaily, TargetValue: 1}, nil
		},
		update: func(context.Context, UpdateParams) (Habit, bool, error) { return Habit{}, false, nil },
	}

	_, err := NewService(repo, nil).Update(context.Background(), "u1", "pro", "h1",
		UpdateHabitRequest{Name: strPtr("x")})
	if got := errCode(err); got != apperror.ErrHabitNotFound {
		t.Errorf("error code = %q, want %q", got, apperror.ErrHabitNotFound)
	}
}

// ── Delete ───────────────────────────────────────────────────────────────────

func TestDelete_MissingHabit(t *testing.T) {
	repo := &mockRepo{archive: func(context.Context, string, string) (bool, error) { return false, nil }}

	err := NewService(repo, nil).Delete(context.Background(), "u1", "nope")
	if got := errCode(err); got != apperror.ErrHabitNotFound {
		t.Errorf("error code = %q, want %q", got, apperror.ErrHabitNotFound)
	}
}

// ── List ─────────────────────────────────────────────────────────────────────

// An empty list is an empty array, never null — a null would break a client
// iterating the response without a guard.
func TestList_EmptyIsNeverNull(t *testing.T) {
	views, err := NewService(&mockRepo{}, nil).List(context.Background(), "u1", false)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if views == nil {
		t.Error("views is nil, want an empty slice")
	}
}

func TestList_PassesIncludeArchivedThrough(t *testing.T) {
	var got bool
	repo := &mockRepo{list: func(_ context.Context, _ string, includeArchived bool) ([]Habit, error) {
		got = includeArchived
		return nil, nil
	}}

	if _, err := NewService(repo, nil).List(context.Background(), "u1", true); err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if !got {
		t.Error("includeArchived = false, want true passed through to the repository")
	}
}

// ── Events ───────────────────────────────────────────────────────────────────

func TestMutations_Broadcast(t *testing.T) {
	bc := &recordingBroadcaster{}
	repo := &mockRepo{getByID: func(context.Context, string, string) (Habit, error) {
		return Habit{ID: "h1", UserID: "u1", Name: "Read", ScheduleKind: ScheduleDaily, TargetValue: 1}, nil
	}}
	svc := NewService(repo, bc)
	ctx := context.Background()

	if _, err := svc.Create(ctx, "u1", "pro", CreateHabitRequest{Name: "Read"}); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := svc.Update(ctx, "u1", "pro", "h1", UpdateHabitRequest{Name: strPtr("Read more")}); err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if err := svc.Delete(ctx, "u1", "h1"); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	want := []string{EventCreated, EventUpdated, EventDeleted}
	if len(bc.events) != len(want) {
		t.Fatalf("got %d events, want %d", len(bc.events), len(want))
	}
	for i, w := range want {
		if bc.events[i].Type != w {
			t.Errorf("event[%d] = %q, want %q", i, bc.events[i].Type, w)
		}
	}
	// The delete payload carries only the id — never stale habit metadata.
	if p, ok := bc.events[2].Payload.(DeletedPayload); !ok || p.ID != "h1" {
		t.Errorf("delete payload = %#v, want DeletedPayload{ID: \"h1\"}", bc.events[2].Payload)
	}
}

// A nil broadcaster is a valid no-op seam, not a panic.
func TestNilBroadcasterIsSafe(t *testing.T) {
	if _, err := NewService(&mockRepo{}, nil).Create(context.Background(), "u1", "pro",
		CreateHabitRequest{Name: "Read"}); err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
}
