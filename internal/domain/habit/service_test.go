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
	list          func(ctx context.Context, userID string, includeArchived bool) ([]Habit, error)
	getByID       func(ctx context.Context, userID, id string) (Habit, error)
	create        func(ctx context.Context, h Habit) (Habit, error)
	update        func(ctx context.Context, p UpdateParams) (Habit, bool, error)
	archive       func(ctx context.Context, userID, id string) (bool, error)
	countActive   func(ctx context.Context, userID string) (int, error)
	upsertCheckIn func(ctx context.Context, c CheckIn) (CheckIn, error)
	deleteCheckIn func(ctx context.Context, userID, habitID string, date time.Time) (bool, error)
	userTimezone  func(ctx context.Context, userID string) (string, error)
	listCheckIns  func(ctx context.Context, userID string, habitIDs []string, since time.Time) (map[string][]CheckIn, error)
}

func (m *mockRepo) ListCheckIns(ctx context.Context, userID string, habitIDs []string, since time.Time) (map[string][]CheckIn, error) {
	if m.listCheckIns != nil {
		return m.listCheckIns(ctx, userID, habitIDs, since)
	}
	return map[string][]CheckIn{}, nil
}

func (m *mockRepo) UpsertCheckIn(ctx context.Context, c CheckIn) (CheckIn, error) {
	if m.upsertCheckIn != nil {
		return m.upsertCheckIn(ctx, c)
	}
	return c, nil
}

func (m *mockRepo) DeleteCheckIn(ctx context.Context, userID, habitID string, date time.Time) (bool, error) {
	if m.deleteCheckIn != nil {
		return m.deleteCheckIn(ctx, userID, habitID, date)
	}
	return true, nil
}

func (m *mockRepo) UserTimezone(ctx context.Context, userID string) (string, error) {
	if m.userTimezone != nil {
		return m.userTimezone(ctx, userID)
	}
	return "UTC", nil
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

// ── Check-in ─────────────────────────────────────────────────────────────────

// pinnedAt builds a service whose clock is fixed, so the local-date rules are
// provable rather than dependent on when the suite happens to run.
func pinnedAt(repo *mockRepo, instant time.Time) Service {
	return newPinnedService(repo, nil, instant)
}

func newPinnedService(repo *mockRepo, bc Broadcaster, instant time.Time) Service {
	return NewServiceWithClock(repo, bc, func() time.Time { return instant })
}

func dailyHabit() Habit {
	return Habit{ID: "h1", UserID: "u1", Name: "Read", Polarity: PolarityBuild,
		TargetValue: 1, ScheduleKind: ScheduleDaily}
}

func repoFor(h Habit, tz string) *mockRepo {
	return &mockRepo{
		getByID:      func(context.Context, string, string) (Habit, error) { return h, nil },
		userTimezone: func(context.Context, string) (string, error) { return tz, nil },
	}
}

// AC1: an empty body checks in for today at the habit's target.
func TestCheckIn_DefaultsToTodayAndTarget(t *testing.T) {
	h := dailyHabit()
	h.TargetValue = 20

	var got CheckIn
	repo := repoFor(h, "UTC")
	repo.upsertCheckIn = func(_ context.Context, c CheckIn) (CheckIn, error) { got = c; return c, nil }

	svc := pinnedAt(repo, time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC))
	if _, err := svc.CheckIn(context.Background(), "u1", "h1", CheckInRequest{}); err != nil {
		t.Fatalf("CheckIn() error = %v, want nil", err)
	}

	if got.Date.Format(DateLayout) != "2026-08-05" {
		t.Errorf("date = %s, want 2026-08-05", got.Date.Format(DateLayout))
	}
	if got.Value != 20 {
		t.Errorf("value = %d, want the habit's target 20", got.Value)
	}
	if !got.Satisfied {
		t.Error("satisfied = false, want true when the value meets the target")
	}
	if got.TargetAt != 20 {
		t.Errorf("targetAtCheckIn = %d, want 20 frozen onto the row", got.TargetAt)
	}
}

// AC2: the server resolves "today" from the user's zone. The naive UTC answer
// here is the previous day, which would credit the check-in to the wrong date
// and silently break the streak.
func TestCheckIn_UsesTheUsersLocalDate(t *testing.T) {
	tests := []struct {
		name    string
		zone    string
		instant time.Time
		want    string
	}{
		{
			name:    "ahead of utc",
			zone:    "Pacific/Auckland",
			instant: time.Date(2026, 8, 4, 21, 0, 0, 0, time.UTC), // 09:00 Aug 5 local
			want:    "2026-08-05",
		},
		{
			name:    "behind utc",
			zone:    "America/Los_Angeles",
			instant: time.Date(2026, 8, 6, 6, 0, 0, 0, time.UTC), // 23:00 Aug 5 local
			want:    "2026-08-05",
		},
		{
			name:    "utc",
			zone:    "UTC",
			instant: time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC),
			want:    "2026-08-05",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got CheckIn
			repo := repoFor(dailyHabit(), tt.zone)
			repo.upsertCheckIn = func(_ context.Context, c CheckIn) (CheckIn, error) { got = c; return c, nil }

			if _, err := pinnedAt(repo, tt.instant).CheckIn(
				context.Background(), "u1", "h1", CheckInRequest{}); err != nil {
				t.Fatalf("CheckIn() error = %v, want nil", err)
			}
			if d := got.Date.Format(DateLayout); d != tt.want {
				t.Errorf("date = %s, want %s (the user's local day)", d, tt.want)
			}
		})
	}
}

// An unresolvable stored zone must not lock the user out of their own habit.
func TestCheckIn_UnknownTimezoneFallsBackToUTC(t *testing.T) {
	var got CheckIn
	repo := repoFor(dailyHabit(), "Mars/Olympus_Mons")
	repo.upsertCheckIn = func(_ context.Context, c CheckIn) (CheckIn, error) { got = c; return c, nil }

	if _, err := pinnedAt(repo, time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)).CheckIn(
		context.Background(), "u1", "h1", CheckInRequest{}); err != nil {
		t.Fatalf("CheckIn() error = %v, want nil", err)
	}
	if got.Date.Format(DateLayout) != "2026-08-05" {
		t.Errorf("date = %s, want the UTC fallback 2026-08-05", got.Date.Format(DateLayout))
	}
}

// AC7: polarity decides the comparison, so a quit habit passes at zero and
// fails the moment a slip is logged.
func TestCheckIn_PolarityDecidesSatisfaction(t *testing.T) {
	tests := []struct {
		name     string
		polarity string
		target   int
		value    int
		want     bool
	}{
		{name: "build habit meets its target", polarity: PolarityBuild, target: 20, value: 20, want: true},
		{name: "build habit falls short", polarity: PolarityBuild, target: 20, value: 5, want: false},
		{name: "quit habit stays clean", polarity: PolarityQuit, target: 0, value: 0, want: true},
		{name: "quit habit slips", polarity: PolarityQuit, target: 0, value: 1, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := dailyHabit()
			h.Polarity, h.TargetValue = tt.polarity, tt.target

			var got CheckIn
			repo := repoFor(h, "UTC")
			repo.upsertCheckIn = func(_ context.Context, c CheckIn) (CheckIn, error) { got = c; return c, nil }

			if _, err := pinnedAt(repo, time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)).CheckIn(
				context.Background(), "u1", "h1", CheckInRequest{Value: intPtr(tt.value)}); err != nil {
				t.Fatalf("CheckIn() error = %v, want nil", err)
			}
			if got.Satisfied != tt.want {
				t.Errorf("satisfied = %v, want %v", got.Satisfied, tt.want)
			}
		})
	}
}

// AC5: backfill is bounded. Unbounded correction would let a user fabricate a
// year-long streak in one sitting.
func TestCheckIn_BackfillWindow(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC) // Wednesday

	tests := []struct {
		name     string
		habit    Habit
		date     string
		wantCode string
	}{
		{name: "seven days back is allowed", habit: dailyHabit(), date: "2026-07-29"},
		{name: "eight days back is refused", habit: dailyHabit(), date: "2026-07-28", wantCode: apperror.ErrInvalidInput},
		{name: "tomorrow is refused", habit: dailyHabit(), date: "2026-08-06", wantCode: apperror.ErrInvalidInput},
		{name: "malformed date", habit: dailyHabit(), date: "05/08/2026", wantCode: apperror.ErrInvalidInput},
		{
			name:  "quota habit, previous week is allowed",
			habit: Habit{ID: "h1", UserID: "u1", Polarity: PolarityBuild, TargetValue: 1, ScheduleKind: ScheduleWeeklyQuota, TimesPerWeek: i16Ptr(3)},
			date:  "2026-07-27",
		},
		{
			name:     "quota habit, two weeks back is refused",
			habit:    Habit{ID: "h1", UserID: "u1", Polarity: PolarityBuild, TargetValue: 1, ScheduleKind: ScheduleWeeklyQuota, TimesPerWeek: i16Ptr(3)},
			date:     "2026-07-26",
			wantCode: apperror.ErrInvalidInput,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pinnedAt(repoFor(tt.habit, "UTC"), now).CheckIn(
				context.Background(), "u1", "h1", CheckInRequest{Date: strPtr(tt.date)})
			if got := errCode(err); got != tt.wantCode {
				t.Errorf("error code = %q, want %q (err=%v)", got, tt.wantCode, err)
			}
		})
	}
}

func TestCheckIn_NegativeValueRejected(t *testing.T) {
	_, err := pinnedAt(repoFor(dailyHabit(), "UTC"), time.Now()).CheckIn(
		context.Background(), "u1", "h1", CheckInRequest{Value: intPtr(-1)})
	if got := errCode(err); got != apperror.ErrInvalidInput {
		t.Errorf("error code = %q, want %q", got, apperror.ErrInvalidInput)
	}
}

// Archived habits are readable history, not a live surface.
func TestCheckIn_ArchivedHabitRejected(t *testing.T) {
	archived := time.Now().UTC()
	h := dailyHabit()
	h.ArchivedAt = &archived

	_, err := pinnedAt(repoFor(h, "UTC"), time.Now()).CheckIn(
		context.Background(), "u1", "h1", CheckInRequest{})
	if got := errCode(err); got != apperror.ErrInvalidInput {
		t.Errorf("error code = %q, want %q", got, apperror.ErrInvalidInput)
	}
}

func TestCheckIn_MissingHabit(t *testing.T) {
	repo := &mockRepo{getByID: func(context.Context, string, string) (Habit, error) {
		return Habit{}, notFound()
	}}

	_, err := pinnedAt(repo, time.Now()).CheckIn(context.Background(), "u1", "nope", CheckInRequest{})
	if got := errCode(err); got != apperror.ErrHabitNotFound {
		t.Errorf("error code = %q, want %q", got, apperror.ErrHabitNotFound)
	}
}

// AC3: a repeat check-in is an upsert, so a double-tap updates rather than
// duplicating. The single write is what the unique index makes idempotent.
func TestCheckIn_IsIdempotentPerDate(t *testing.T) {
	var writes []CheckIn
	repo := repoFor(dailyHabit(), "UTC")
	repo.upsertCheckIn = func(_ context.Context, c CheckIn) (CheckIn, error) {
		writes = append(writes, c)
		return c, nil
	}

	svc := pinnedAt(repo, time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC))
	ctx := context.Background()
	if _, err := svc.CheckIn(ctx, "u1", "h1", CheckInRequest{}); err != nil {
		t.Fatalf("first CheckIn: %v", err)
	}
	if _, err := svc.CheckIn(ctx, "u1", "h1", CheckInRequest{Value: intPtr(3)}); err != nil {
		t.Fatalf("second CheckIn: %v", err)
	}

	if len(writes) != 2 {
		t.Fatalf("got %d writes, want 2", len(writes))
	}
	if !writes[0].Date.Equal(writes[1].Date) {
		t.Error("the two writes targeted different dates, want the same day upserted")
	}
	if writes[1].Value != 3 {
		t.Errorf("second value = %d, want 3", writes[1].Value)
	}
}

// ── Undo ─────────────────────────────────────────────────────────────────────

// AC4: undo removes the day and is safe to repeat — the caller wanted the day
// not-done, and after the first call it already is.
func TestUndoCheckIn_IsIdempotent(t *testing.T) {
	repo := repoFor(dailyHabit(), "UTC")
	repo.deleteCheckIn = func(context.Context, string, string, time.Time) (bool, error) { return false, nil }

	_, err := pinnedAt(repo, time.Now()).UndoCheckIn(context.Background(), "u1", "h1", UndoCheckInRequest{})
	if err != nil {
		t.Errorf("UndoCheckIn() error = %v, want nil when no entry existed", err)
	}
}

func TestUndoCheckIn_TargetsTheResolvedDate(t *testing.T) {
	var gotDate time.Time
	repo := repoFor(dailyHabit(), "Pacific/Auckland")
	repo.deleteCheckIn = func(_ context.Context, _, _ string, d time.Time) (bool, error) {
		gotDate = d
		return true, nil
	}

	// 09:00 Aug 5 in Auckland — the UTC date is still Aug 4.
	svc := pinnedAt(repo, time.Date(2026, 8, 4, 21, 0, 0, 0, time.UTC))
	if _, err := svc.UndoCheckIn(context.Background(), "u1", "h1", UndoCheckInRequest{}); err != nil {
		t.Fatalf("UndoCheckIn() error = %v, want nil", err)
	}
	if gotDate.Format(DateLayout) != "2026-08-05" {
		t.Errorf("deleted date = %s, want the local day 2026-08-05", gotDate.Format(DateLayout))
	}
}

func TestUndoCheckIn_HonoursTheBackfillWindow(t *testing.T) {
	_, err := pinnedAt(repoFor(dailyHabit(), "UTC"), time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)).
		UndoCheckIn(context.Background(), "u1", "h1", UndoCheckInRequest{Date: strPtr("2026-07-01")})
	if got := errCode(err); got != apperror.ErrInvalidInput {
		t.Errorf("error code = %q, want %q", got, apperror.ErrInvalidInput)
	}
}

func TestCheckInAndUndo_Broadcast(t *testing.T) {
	bc := &recordingBroadcaster{}
	repo := repoFor(dailyHabit(), "UTC")
	svc := newPinnedService(repo, bc, time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC))

	ctx := context.Background()
	if _, err := svc.CheckIn(ctx, "u1", "h1", CheckInRequest{}); err != nil {
		t.Fatalf("CheckIn: %v", err)
	}
	if _, err := svc.UndoCheckIn(ctx, "u1", "h1", UndoCheckInRequest{}); err != nil {
		t.Fatalf("UndoCheckIn: %v", err)
	}

	if len(bc.events) != 2 {
		t.Fatalf("got %d events, want 2", len(bc.events))
	}
	for i, ev := range bc.events {
		if ev.Type != EventCheckedIn {
			t.Errorf("event[%d] = %q, want %q", i, ev.Type, EventCheckedIn)
		}
	}
}

// ── Enrichment ───────────────────────────────────────────────────────────────

// A list read must derive every habit's streak from one history query. N+1 here
// is what would make derived-on-read expensive enough to regret.
func TestList_LoadsHistoryInOneQuery(t *testing.T) {
	habits := []Habit{
		{ID: "h1", UserID: "u1", Name: "Read", Polarity: PolarityBuild, TargetValue: 1, ScheduleKind: ScheduleDaily},
		{ID: "h2", UserID: "u1", Name: "Run", Polarity: PolarityBuild, TargetValue: 1, ScheduleKind: ScheduleDaily},
		{ID: "h3", UserID: "u1", Name: "Water", Polarity: PolarityBuild, TargetValue: 1, ScheduleKind: ScheduleDaily},
	}
	today := date(2026, time.August, 5)

	calls := 0
	var gotIDs []string
	repo := &mockRepo{
		list: func(context.Context, string, bool) ([]Habit, error) { return habits, nil },
		listCheckIns: func(_ context.Context, _ string, ids []string, _ time.Time) (map[string][]CheckIn, error) {
			calls++
			gotIDs = ids
			return map[string][]CheckIn{"h1": run(today, 4)}, nil
		},
	}

	views, err := pinnedAt(repo, time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)).
		List(context.Background(), "u1", false)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if calls != 1 {
		t.Errorf("ListCheckIns called %d times, want exactly 1 for %d habits", calls, len(habits))
	}
	if len(gotIDs) != 3 {
		t.Errorf("requested %d habit ids, want all 3 batched", len(gotIDs))
	}
	if views[0].CurrentStreak != 4 {
		t.Errorf("h1 streak = %d, want 4", views[0].CurrentStreak)
	}
	if views[1].CurrentStreak != 0 {
		t.Errorf("h2 streak = %d, want 0 — it has no history", views[1].CurrentStreak)
	}
}

// An empty list must not issue a history query at all.
func TestList_NoHabitsSkipsTheHistoryQuery(t *testing.T) {
	called := false
	repo := &mockRepo{listCheckIns: func(context.Context, string, []string, time.Time) (map[string][]CheckIn, error) {
		called = true
		return nil, nil
	}}

	views, err := pinnedAt(repo, time.Now()).List(context.Background(), "u1", false)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if views == nil {
		t.Error("views is nil, want an empty slice")
	}
	if called {
		t.Error("ListCheckIns was called for a user with no habits, want it skipped")
	}
}

func TestList_StreakUnitFollowsTheSchedule(t *testing.T) {
	three := int16(3)
	repo := &mockRepo{list: func(context.Context, string, bool) ([]Habit, error) {
		return []Habit{
			{ID: "h1", ScheduleKind: ScheduleDaily},
			{ID: "h2", ScheduleKind: ScheduleWeekdays, ByWeekday: []int16{1}},
			{ID: "h3", ScheduleKind: ScheduleWeeklyQuota, TimesPerWeek: &three},
		}, nil
	}}

	views, err := pinnedAt(repo, time.Now()).List(context.Background(), "u1", false)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	want := []string{StreakUnitDay, StreakUnitDay, StreakUnitWeek}
	for i, w := range want {
		if views[i].StreakUnit != w {
			t.Errorf("views[%d].streakUnit = %q, want %q", i, views[i].StreakUnit, w)
		}
	}
	// Only the quota habit accumulates toward a period.
	if views[0].PeriodProgress != nil || views[1].PeriodProgress != nil {
		t.Error("a day habit carries periodProgress, want nil")
	}
	if views[2].PeriodProgress == nil {
		t.Error("the quota habit has no periodProgress, want it populated")
	}
}

// An archived habit is history: it is never due, whatever its schedule says.
func TestList_ArchivedHabitIsNeverDue(t *testing.T) {
	archived := time.Now().UTC()
	repo := &mockRepo{list: func(context.Context, string, bool) ([]Habit, error) {
		return []Habit{{ID: "h1", ScheduleKind: ScheduleDaily, ArchivedAt: &archived}}, nil
	}}

	views, err := pinnedAt(repo, time.Now()).List(context.Background(), "u1", true)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}
	if views[0].DueToday {
		t.Error("dueToday = true on an archived habit, want false")
	}
}

func TestGet_ReturnsCellsAtTheHabitsGranularity(t *testing.T) {
	today := date(2026, time.August, 5)
	repo := &mockRepo{
		getByID: func(context.Context, string, string) (Habit, error) {
			return Habit{ID: "h1", UserID: "u1", Polarity: PolarityBuild, TargetValue: 1,
				ScheduleKind: ScheduleDaily}, nil
		},
		listCheckIns: func(context.Context, string, []string, time.Time) (map[string][]CheckIn, error) {
			return map[string][]CheckIn{"h1": run(today, 3)}, nil
		},
	}

	got, err := pinnedAt(repo, time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)).
		Get(context.Background(), "u1", "h1")
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}

	if len(got.Cells) != RibbonDays {
		t.Errorf("got %d cells, want %d", len(got.Cells), RibbonDays)
	}
	if got.CurrentStreak != 3 {
		t.Errorf("currentStreak = %d, want 3", got.CurrentStreak)
	}
	if last := got.Cells[len(got.Cells)-1]; last.Date != "2026-08-05" || !last.Satisfied {
		t.Errorf("last cell = %+v, want today satisfied", last)
	}
}

// The response to a check-in carries the recomputed streak, so a client does not
// need a follow-up fetch and a second tab gets it straight from the broadcast.
func TestCheckIn_ReturnsTheRecomputedStreak(t *testing.T) {
	today := date(2026, time.August, 5)
	bc := &recordingBroadcaster{}

	repo := repoFor(dailyHabit(), "UTC")
	repo.listCheckIns = func(context.Context, string, []string, time.Time) (map[string][]CheckIn, error) {
		// The four days behind today, plus today itself just written.
		return map[string][]CheckIn{"h1": run(today, 5)}, nil
	}

	svc := newPinnedService(repo, bc, time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC))
	view, err := svc.CheckIn(context.Background(), "u1", "h1", CheckInRequest{})
	if err != nil {
		t.Fatalf("CheckIn() error = %v, want nil", err)
	}

	if view.CurrentStreak != 5 {
		t.Errorf("currentStreak = %d, want 5 recomputed after the write", view.CurrentStreak)
	}
	if !view.CompletedToday {
		t.Error("completedToday = false after checking in, want true")
	}

	broadcast, ok := bc.events[0].Payload.(HabitView)
	if !ok {
		t.Fatalf("payload = %#v, want a HabitView", bc.events[0].Payload)
	}
	if broadcast.CurrentStreak != 5 {
		t.Errorf("broadcast streak = %d, want the post-write 5", broadcast.CurrentStreak)
	}
}

// ── Today feed ───────────────────────────────────────────────────────────────

func TestToday_FiltersByScheduleAndCompletion(t *testing.T) {
	// A Wednesday. The Mon/Wed/Fri habit is due; a Tue/Thu one would not be.
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	today := date(2026, time.August, 5)
	archived := now

	habits := []Habit{
		{ID: "daily", ScheduleKind: ScheduleDaily, Polarity: PolarityBuild, TargetValue: 1},
		{ID: "onSchedule", ScheduleKind: ScheduleWeekdays, ByWeekday: []int16{1, 3, 5}, Polarity: PolarityBuild, TargetValue: 1},
		{ID: "offSchedule", ScheduleKind: ScheduleWeekdays, ByWeekday: []int16{2, 4}, Polarity: PolarityBuild, TargetValue: 1},
		{ID: "alreadyDone", ScheduleKind: ScheduleDaily, Polarity: PolarityBuild, TargetValue: 1},
		{ID: "archived", ScheduleKind: ScheduleDaily, Polarity: PolarityBuild, TargetValue: 1, ArchivedAt: &archived},
	}

	repo := &mockRepo{
		list: func(_ context.Context, _ string, includeArchived bool) ([]Habit, error) {
			if includeArchived {
				return habits, nil
			}
			return habits[:4], nil
		},
		listCheckIns: func(context.Context, string, []string, time.Time) (map[string][]CheckIn, error) {
			return map[string][]CheckIn{"alreadyDone": {ci(today, 1, true)}}, nil
		},
	}

	views, err := pinnedAt(repo, now).Today(context.Background(), "u1")
	if err != nil {
		t.Fatalf("Today() error = %v, want nil", err)
	}

	got := map[string]bool{}
	for _, v := range views {
		got[v.ID] = true
	}
	if !got["daily"] || !got["onSchedule"] {
		t.Errorf("feed = %v, want the daily and on-schedule habits present", got)
	}
	if got["offSchedule"] {
		t.Error("an off-schedule habit appeared in the feed, want it excluded")
	}
	if got["alreadyDone"] {
		t.Error("a completed habit appeared in the feed, want it excluded")
	}
	if got["archived"] {
		t.Error("an archived habit appeared in the feed, want it excluded")
	}
}

// A quota habit nags every day until the week is met, then goes quiet rather
// than asking for a fourth session.
func TestToday_QuotaHabitLeavesTheFeedWhenMet(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	week := date(2026, time.August, 3)

	newRepo := func(done int) *mockRepo {
		entries := make([]CheckIn, 0, done)
		for i := range done {
			entries = append(entries, ci(week.AddDate(0, 0, i), 1, true))
		}
		return &mockRepo{
			list: func(context.Context, string, bool) ([]Habit, error) {
				return []Habit{{ID: "h1", ScheduleKind: ScheduleWeeklyQuota,
					TimesPerWeek: i16Ptr(3), Polarity: PolarityBuild, TargetValue: 1}}, nil
			},
			listCheckIns: func(context.Context, string, []string, time.Time) (map[string][]CheckIn, error) {
				return map[string][]CheckIn{"h1": entries}, nil
			},
		}
	}

	partial, err := pinnedAt(newRepo(2), now).Today(context.Background(), "u1")
	if err != nil {
		t.Fatalf("Today() error = %v", err)
	}
	if len(partial) != 1 {
		t.Errorf("feed has %d habits at 2 of 3, want 1", len(partial))
	}

	met, err := pinnedAt(newRepo(3), now).Today(context.Background(), "u1")
	if err != nil {
		t.Fatalf("Today() error = %v", err)
	}
	if len(met) != 0 {
		t.Errorf("feed has %d habits at 3 of 3, want none", len(met))
	}
}

func TestToday_EmptyIsNeverNull(t *testing.T) {
	views, err := pinnedAt(&mockRepo{}, time.Now()).Today(context.Background(), "u1")
	if err != nil {
		t.Fatalf("Today() error = %v, want nil", err)
	}
	if views == nil {
		t.Error("views is nil, want an empty slice")
	}
}

// ── Subject catalog ──────────────────────────────────────────────────────────

func TestSubjectCatalog(t *testing.T) {
	if len(SubjectCatalog) < 20 {
		t.Errorf("catalog has %d entries, want at least 20", len(SubjectCatalog))
	}

	seen := map[string]bool{}
	for _, s := range SubjectCatalog {
		if s.Slug == "" || s.LabelKey == "" {
			t.Errorf("entry %+v has an empty field", s)
		}
		if seen[s.Slug] {
			t.Errorf("duplicate slug %q", s.Slug)
		}
		seen[s.Slug] = true
	}

	// The default subject must exist in the catalog, or a habit created without
	// one would render as an unknown slug.
	if !seen[DefaultSubject] {
		t.Errorf("catalog is missing the default subject %q", DefaultSubject)
	}
	// The quit-habit examples the feature was designed around.
	for _, want := range []string{"reading", "quit_drinking", "quit_smoking"} {
		if !seen[want] {
			t.Errorf("catalog is missing %q", want)
		}
	}
}

// ── Heatmap windows ──────────────────────────────────────────────────────────

// The board draws one ribbon per card, so the list has to carry cells. It costs
// nothing: List already loads HistoryWindow days per habit to derive the
// streaks, and before this the rows were walked once and discarded.
func TestList_CarriesTheNarrowRibbonWindow(t *testing.T) {
	today := date(2026, time.August, 5)
	repo := &mockRepo{
		list: func(context.Context, string, bool) ([]Habit, error) {
			return []Habit{{ID: "h1", Polarity: PolarityBuild, TargetValue: 1, ScheduleKind: ScheduleDaily}}, nil
		},
		listCheckIns: func(context.Context, string, []string, time.Time) (map[string][]CheckIn, error) {
			return map[string][]CheckIn{"h1": run(today, 3)}, nil
		},
	}

	views, err := pinnedAt(repo, time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)).
		List(context.Background(), "u1", false)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if len(views[0].Cells) != ListRibbonDays {
		t.Errorf("list cells = %d, want %d", len(views[0].Cells), ListRibbonDays)
	}
	if last := views[0].Cells[len(views[0].Cells)-1]; last.Date != "2026-08-05" || !last.Satisfied {
		t.Errorf("last cell = %+v, want today satisfied", last)
	}
}

// The scalar read is the detail page's window and stays wide, so the two reads
// differ only in how much history they carry.
func TestGet_CarriesTheWideRibbonWindow(t *testing.T) {
	today := date(2026, time.August, 5)
	repo := &mockRepo{
		getByID: func(context.Context, string, string) (Habit, error) {
			return Habit{ID: "h1", UserID: "u1", Polarity: PolarityBuild, TargetValue: 1,
				ScheduleKind: ScheduleDaily}, nil
		},
		listCheckIns: func(context.Context, string, []string, time.Time) (map[string][]CheckIn, error) {
			return map[string][]CheckIn{"h1": run(today, 3)}, nil
		},
	}

	got, err := pinnedAt(repo, time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)).
		Get(context.Background(), "u1", "h1")
	if err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}

	if len(got.Cells) != RibbonDays {
		t.Errorf("scalar cells = %d, want %d", len(got.Cells), RibbonDays)
	}
	if RibbonDays <= ListRibbonDays {
		t.Error("the scalar window is not wider than the list window")
	}
}

// A check-in usually happens on a detail page, so shipping the narrow window
// back would shrink the ribbon under the user's finger the moment they tapped.
func TestCheckIn_ResponseCarriesTheWideWindow(t *testing.T) {
	today := date(2026, time.August, 5)
	repo := repoFor(dailyHabit(), "UTC")
	repo.listCheckIns = func(context.Context, string, []string, time.Time) (map[string][]CheckIn, error) {
		return map[string][]CheckIn{"h1": run(today, 5)}, nil
	}

	view, err := pinnedAt(repo, time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)).
		CheckIn(context.Background(), "u1", "h1", CheckInRequest{})
	if err != nil {
		t.Fatalf("CheckIn() error = %v, want nil", err)
	}

	if len(view.Cells) != RibbonDays {
		t.Errorf("check-in cells = %d, want the scalar window %d", len(view.Cells), RibbonDays)
	}
}

// Every habit in a list read gets its own window, keyed correctly — a shared or
// mis-keyed slice would draw one habit's history on another's card.
func TestList_CellsArePerHabit(t *testing.T) {
	today := date(2026, time.August, 5)
	repo := &mockRepo{
		list: func(context.Context, string, bool) ([]Habit, error) {
			return []Habit{
				{ID: "h1", Polarity: PolarityBuild, TargetValue: 1, ScheduleKind: ScheduleDaily},
				{ID: "h2", Polarity: PolarityBuild, TargetValue: 1, ScheduleKind: ScheduleDaily},
			}, nil
		},
		listCheckIns: func(context.Context, string, []string, time.Time) (map[string][]CheckIn, error) {
			return map[string][]CheckIn{"h1": run(today, 3)}, nil
		},
	}

	views, err := pinnedAt(repo, time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)).
		List(context.Background(), "u1", false)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	satisfied := func(cells []CellView) int {
		n := 0
		for _, c := range cells {
			if c.Satisfied {
				n++
			}
		}
		return n
	}

	if got := satisfied(views[0].Cells); got != 3 {
		t.Errorf("h1 satisfied cells = %d, want 3", got)
	}
	if got := satisfied(views[1].Cells); got != 0 {
		t.Errorf("h2 satisfied cells = %d, want 0 — it has no history", got)
	}
}

// A quota habit's window is weeks, not days, at both widths.
func TestList_QuotaHabitGetsWeekCells(t *testing.T) {
	week := date(2026, time.August, 3)
	repo := &mockRepo{
		list: func(context.Context, string, bool) ([]Habit, error) {
			return []Habit{{ID: "h1", Polarity: PolarityBuild, TargetValue: 1,
				ScheduleKind: ScheduleWeeklyQuota, TimesPerWeek: i16Ptr(3)}}, nil
		},
		listCheckIns: func(context.Context, string, []string, time.Time) (map[string][]CheckIn, error) {
			return map[string][]CheckIn{"h1": {ci(week, 1, true), ci(week.AddDate(0, 0, 1), 1, true)}}, nil
		},
	}

	views, err := pinnedAt(repo, time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)).
		List(context.Background(), "u1", false)
	if err != nil {
		t.Fatalf("List() error = %v, want nil", err)
	}

	if len(views[0].Cells) != ListRibbonDays/7 {
		t.Errorf("quota cells = %d, want %d week cells", len(views[0].Cells), ListRibbonDays/7)
	}
	last := views[0].Cells[len(views[0].Cells)-1]
	if last.Progress == nil || last.Progress.Current != 2 || last.Progress.Target != 3 {
		t.Errorf("last week progress = %+v, want 2 of 3", last.Progress)
	}
}

// A nil broadcaster is a valid no-op seam, not a panic.
func TestNilBroadcasterIsSafe(t *testing.T) {
	if _, err := NewService(&mockRepo{}, nil).Create(context.Background(), "u1", "pro",
		CreateHabitRequest{Name: "Read"}); err != nil {
		t.Fatalf("Create() error = %v, want nil", err)
	}
}
