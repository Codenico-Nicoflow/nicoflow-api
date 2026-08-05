//go:build integration

package habit_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/habit"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
)

const testEmailSuffix = "@habit.integration.test"

func cleanTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	// habit_check_ins and habits cascade from users, but delete leaf-first anyway
	// so a partial failure can't leave orphans behind for the next run.
	queries := []string{
		`DELETE FROM habit_check_ins WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%' || $1)`,
		`DELETE FROM habits WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%' || $1)`,
		`DELETE FROM users WHERE email LIKE '%' || $1`,
	}
	for _, q := range queries {
		if _, err := pool.Exec(context.Background(), q, testEmailSuffix); err != nil {
			t.Fatalf("cleanTestData: %v", err)
		}
	}
}

func newRepo(t *testing.T) (habit.Repository, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewTestDB(t)
	cleanTestData(t, pool)
	t.Cleanup(func() { cleanTestData(t, pool) })
	return habit.NewRepository(pool), pool
}

func seedUser(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	id := uuid.NewString()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, username, password_hash, plan)
		 VALUES ($1, $2, $3, 'x', 'free')`,
		id, id+testEmailSuffix, "u_"+id[:8],
	)
	if err != nil {
		t.Fatalf("seedUser: %v", err)
	}
	return id
}

func newHabit(userID, name string) habit.Habit {
	return habit.Habit{
		ID: uuid.NewString(), UserID: userID, Name: name,
		Subject: habit.DefaultSubject, Color: habit.DefaultColor,
		Polarity: habit.PolarityBuild, TargetValue: 1,
		ScheduleKind: habit.ScheduleDaily,
	}
}

func i16Ptr(v int16) *int16 { return &v }
func boolPtr(v bool) *bool  { return &v }

func TestCreateAndGet(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)

	in := newHabit(userID, "Read")
	unit := "minutes"
	in.TargetValue, in.Unit = 20, &unit

	created, err := repo.Create(ctx, in)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.CreatedAt.IsZero() || created.UpdatedAt.IsZero() {
		t.Error("timestamps are zero, want them stamped by the column defaults")
	}
	if created.ArchivedAt != nil {
		t.Errorf("archivedAt = %v, want nil on a fresh habit", created.ArchivedAt)
	}

	got, err := repo.GetByID(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.Name != "Read" || got.TargetValue != 20 || got.Unit == nil || *got.Unit != "minutes" {
		t.Errorf("got %+v, want the stored values round-tripped", got)
	}
}

// A habit belonging to someone else is reported exactly like a missing one, so
// the endpoint can never become an existence oracle.
func TestGetByID_CrossUserIsNotFound(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	owner, other := seedUser(t, pool), seedUser(t, pool)

	created, err := repo.Create(ctx, newHabit(owner, "Read"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, foreignErr := repo.GetByID(ctx, other, created.ID)
	_, missingErr := repo.GetByID(ctx, other, uuid.NewString())

	for name, err := range map[string]error{"foreign": foreignErr, "missing": missingErr} {
		var ae *apperror.AppError
		if !errors.As(err, &ae) || ae.Code != apperror.ErrHabitNotFound {
			t.Errorf("%s habit: error = %v, want HABIT_NOT_FOUND", name, err)
		}
	}
}

func TestList_ExcludesArchivedUnlessAsked(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)

	active, err := repo.Create(ctx, newHabit(userID, "Read"))
	if err != nil {
		t.Fatalf("Create active: %v", err)
	}
	archived, err := repo.Create(ctx, newHabit(userID, "Run"))
	if err != nil {
		t.Fatalf("Create archived: %v", err)
	}
	if _, err := repo.Archive(ctx, userID, archived.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	activeOnly, err := repo.List(ctx, userID, false)
	if err != nil {
		t.Fatalf("List active: %v", err)
	}
	if len(activeOnly) != 1 || activeOnly[0].ID != active.ID {
		t.Errorf("active list = %d rows, want only the unarchived habit", len(activeOnly))
	}

	all, err := repo.List(ctx, userID, true)
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("full list = %d rows, want 2", len(all))
	}
}

func TestList_IsScopedToTheUser(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userA, userB := seedUser(t, pool), seedUser(t, pool)

	if _, err := repo.Create(ctx, newHabit(userA, "Read")); err != nil {
		t.Fatalf("Create for A: %v", err)
	}
	if _, err := repo.Create(ctx, newHabit(userB, "Run")); err != nil {
		t.Fatalf("Create for B: %v", err)
	}

	listA, err := repo.List(ctx, userA, true)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listA) != 1 || listA[0].Name != "Read" {
		t.Errorf("user A sees %d habits, want only their own", len(listA))
	}
}

// The plan limit counts active habits only — which is what lets a user archive
// one habit to make room for another.
func TestCountActive_IgnoresArchived(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)

	for _, name := range []string{"a", "b", "c"} {
		if _, err := repo.Create(ctx, newHabit(userID, name)); err != nil {
			t.Fatalf("Create %s: %v", name, err)
		}
	}
	all, err := repo.List(ctx, userID, false)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if _, err := repo.Archive(ctx, userID, all[0].ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	n, err := repo.CountActive(ctx, userID)
	if err != nil {
		t.Fatalf("CountActive: %v", err)
	}
	if n != 2 {
		t.Errorf("CountActive = %d, want 2 (archived rows excluded)", n)
	}
}

// Archiving twice must not move the original archival instant — a repeated
// DELETE is idempotent, not a reset.
func TestArchive_IsIdempotent(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)

	created, err := repo.Create(ctx, newHabit(userID, "Read"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := repo.Archive(ctx, userID, created.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}
	first, err := repo.GetByID(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	ok, err := repo.Archive(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("Archive again: %v", err)
	}
	if !ok {
		t.Error("second Archive reported no row, want the row still matched")
	}

	second, err := repo.GetByID(ctx, userID, created.ID)
	if err != nil {
		t.Fatalf("GetByID after re-archive: %v", err)
	}
	if !first.ArchivedAt.Equal(*second.ArchivedAt) {
		t.Errorf("archivedAt moved from %v to %v, want the original instant kept",
			first.ArchivedAt, second.ArchivedAt)
	}
}

func TestArchive_CrossUserDoesNothing(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	owner, other := seedUser(t, pool), seedUser(t, pool)

	created, err := repo.Create(ctx, newHabit(owner, "Read"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	ok, err := repo.Archive(ctx, other, created.ID)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if ok {
		t.Error("Archive reported success for a foreign habit, want no row matched")
	}

	still, err := repo.GetByID(ctx, owner, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if still.ArchivedAt != nil {
		t.Error("the owner's habit was archived by another user")
	}
}

// Restoring writes NULL, which COALESCE could not express — the reason Update
// uses a CASE for archived_at.
func TestUpdate_ArchiveThenRestore(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)

	created, err := repo.Create(ctx, newHabit(userID, "Read"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	base := habit.UpdateParams{
		ID: created.ID, UserID: userID, Name: created.Name,
		Subject: created.Subject, Color: created.Color,
		TargetValue: created.TargetValue, ScheduleKind: created.ScheduleKind,
	}

	archiveParams := base
	archiveParams.Archived = boolPtr(true)
	archived, ok, err := repo.Update(ctx, archiveParams)
	if err != nil || !ok {
		t.Fatalf("Update archive: err=%v ok=%v", err, ok)
	}
	if archived.ArchivedAt == nil {
		t.Fatal("archivedAt is nil after archiving, want it set")
	}

	restoreParams := base
	restoreParams.Archived = boolPtr(false)
	restored, ok, err := repo.Update(ctx, restoreParams)
	if err != nil || !ok {
		t.Fatalf("Update restore: err=%v ok=%v", err, ok)
	}
	if restored.ArchivedAt != nil {
		t.Errorf("archivedAt = %v after restore, want nil", restored.ArchivedAt)
	}
}

// A nil Archived leaves archival untouched, so an ordinary edit cannot
// accidentally resurrect or retire a habit.
func TestUpdate_NilArchivedLeavesArchivalAlone(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)

	created, err := repo.Create(ctx, newHabit(userID, "Read"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := repo.Archive(ctx, userID, created.ID); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	updated, ok, err := repo.Update(ctx, habit.UpdateParams{
		ID: created.ID, UserID: userID, Name: "Read more",
		Subject: created.Subject, Color: created.Color,
		TargetValue: created.TargetValue, ScheduleKind: created.ScheduleKind,
	})
	if err != nil || !ok {
		t.Fatalf("Update: err=%v ok=%v", err, ok)
	}
	if updated.ArchivedAt == nil {
		t.Error("archivedAt was cleared by an edit that did not mention archival")
	}
	if updated.Name != "Read more" {
		t.Errorf("name = %q, want the edit applied", updated.Name)
	}
}

func TestUpdate_CrossUserMatchesNothing(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	owner, other := seedUser(t, pool), seedUser(t, pool)

	created, err := repo.Create(ctx, newHabit(owner, "Read"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	_, ok, err := repo.Update(ctx, habit.UpdateParams{
		ID: created.ID, UserID: other, Name: "Hijacked",
		Subject: created.Subject, Color: created.Color,
		TargetValue: 1, ScheduleKind: habit.ScheduleDaily,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if ok {
		t.Fatal("Update matched a foreign habit, want no row")
	}

	still, err := repo.GetByID(ctx, owner, created.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if still.Name != "Read" {
		t.Errorf("name = %q, want the owner's value untouched", still.Name)
	}
}

// The schedule-shape CHECK is the database's own guard: even if the service
// were bypassed, a half-specified schedule cannot be stored.
func TestScheduleShapeConstraint(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)

	tests := []struct {
		name    string
		mutate  func(h *habit.Habit)
		wantErr bool
	}{
		{
			name:    "weekdays without days",
			mutate:  func(h *habit.Habit) { h.ScheduleKind = habit.ScheduleWeekdays },
			wantErr: true,
		},
		{
			name: "weekdays with days",
			mutate: func(h *habit.Habit) {
				h.ScheduleKind = habit.ScheduleWeekdays
				h.ByWeekday = []int16{1, 3, 5}
			},
		},
		{
			name:    "quota without a count",
			mutate:  func(h *habit.Habit) { h.ScheduleKind = habit.ScheduleWeeklyQuota },
			wantErr: true,
		},
		{
			name: "quota with a count",
			mutate: func(h *habit.Habit) {
				h.ScheduleKind = habit.ScheduleWeeklyQuota
				h.TimesPerWeek = i16Ptr(3)
			},
		},
		{
			name:    "quota above 7",
			mutate:  func(h *habit.Habit) { h.ScheduleKind = habit.ScheduleWeeklyQuota; h.TimesPerWeek = i16Ptr(8) },
			wantErr: true,
		},
		{
			name:    "unknown polarity",
			mutate:  func(h *habit.Habit) { h.Polarity = "sideways" },
			wantErr: true,
		},
		{
			name:    "negative target",
			mutate:  func(h *habit.Habit) { h.TargetValue = -1 },
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHabit(userID, tt.name)
			tt.mutate(&h)

			_, err := repo.Create(ctx, h)
			if tt.wantErr {
				var pgErr *pgconn.PgError
				if !errors.As(err, &pgErr) {
					t.Fatalf("error = %v, want a postgres constraint violation", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
		})
	}
}

// Deleting a user takes their habits with them.
func TestUserDeleteCascades(t *testing.T) {
	repo, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)

	if _, err := repo.Create(ctx, newHabit(userID, "Read")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
		t.Fatalf("delete user: %v", err)
	}

	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM habits WHERE user_id = $1`, userID).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("%d habits survived the user deletion, want 0", n)
	}
}
