//go:build integration

package recurrence_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/domain/recurrence"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
)

const testEmailSuffix = "@recurrence.integration.test"

func cleanTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	queries := []string{
		`DELETE FROM tasks WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%' || $1)`,
		`DELETE FROM recurrence_rules WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%' || $1)`,
		`DELETE FROM projects WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%' || $1)`,
		`DELETE FROM areas WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%' || $1)`,
		`DELETE FROM users WHERE email LIKE '%' || $1`,
	}
	for _, q := range queries {
		if _, err := pool.Exec(context.Background(), q, testEmailSuffix); err != nil {
			t.Fatalf("cleanTestData: %v", err)
		}
	}
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

// seedProject creates the area→project chain a rule needs (projects.area_id is
// NOT NULL since migration 027).
func seedProject(t *testing.T, pool *pgxpool.Pool, userID string) string {
	t.Helper()
	ctx := context.Background()
	areaID := uuid.NewString()
	// Names are unique per user, so derive them from the id — a test may seed
	// several projects for the same user.
	if _, err := pool.Exec(ctx,
		`INSERT INTO areas (id, user_id, name) VALUES ($1, $2, $3)`, areaID, userID, "Area "+areaID[:8],
	); err != nil {
		t.Fatalf("seedProject area: %v", err)
	}
	projectID := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO projects (id, user_id, area_id, name) VALUES ($1, $2, $3, $4)`,
		projectID, userID, areaID, "Project "+projectID[:8],
	); err != nil {
		t.Fatalf("seedProject project: %v", err)
	}
	return projectID
}

func newRepo(t *testing.T) (recurrence.Repository, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewTestDB(t)
	cleanTestData(t, pool)
	t.Cleanup(func() { cleanTestData(t, pool) })
	return recurrence.NewRepository(pool), pool
}

func date(s string) time.Time {
	d, err := recurrence.ParseDate(s)
	if err != nil {
		panic(err)
	}
	return d
}

func newRule(userID, projectID string) recurrence.Rule {
	next := date("2026-03-09")
	return recurrence.Rule{
		ID:        uuid.NewString(),
		UserID:    userID,
		ProjectID: projectID,
		Title:     "Water the plants",
		Priority:  "medium",
		Energy:    "medium",
		Freq:      recurrence.FreqWeekly,
		Interval:  1,
		ByWeekday: []int{1},
		StartDate: date("2026-03-02"),

		NextOccurrence: &next,
	}
}

func newOccurrence(r recurrence.Rule, d string) recurrence.Occurrence {
	return recurrence.Occurrence{
		ID:             uuid.NewString(),
		UserID:         r.UserID,
		ProjectID:      r.ProjectID,
		RuleID:         r.ID,
		Title:          r.Title,
		Priority:       r.Priority,
		Energy:         r.Energy,
		OccurrenceDate: date(d),
	}
}

func countTasks(t *testing.T, pool *pgxpool.Pool, ruleID string) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM tasks WHERE recurrence_rule_id = $1`, ruleID,
	).Scan(&n); err != nil {
		t.Fatalf("countTasks: %v", err)
	}
	return n
}

// Creating a rule materializes instance #1 in the same transaction.
func TestRepo_CreateWithOccurrence(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	rule := newRule(userID, projectID)
	got, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02"))
	if err != nil {
		t.Fatalf("CreateWithOccurrence: %v", err)
	}
	if got.ID != rule.ID || got.Title != rule.Title {
		t.Errorf("rule = %+v, want the inserted row", got)
	}
	if len(got.ByWeekday) != 1 || got.ByWeekday[0] != 1 {
		t.Errorf("byWeekday = %v, want [1] round-tripped through SMALLINT[]", got.ByWeekday)
	}
	if n := countTasks(t, pool, rule.ID); n != 1 {
		t.Errorf("occurrence count = %d, want 1", n)
	}

	var status, scheduledFor string
	if err := pool.QueryRow(ctx,
		`SELECT status, scheduled_for FROM tasks WHERE recurrence_rule_id = $1`, rule.ID,
	).Scan(&status, &scheduledFor); err != nil {
		t.Fatalf("read occurrence: %v", err)
	}
	if status != "active" {
		t.Errorf("status = %q, want active", status)
	}
	if scheduledFor != "2026-03-02" {
		t.Errorf("scheduledFor = %q, want 2026-03-02", scheduledFor)
	}
}

// The partial unique index is the idempotency guarantee the cron sweep and the
// sync-on-complete path both rely on: a duplicate (rule, date) must not double-insert.
func TestRepo_DuplicateOccurrenceIsRejected(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	rule := newRule(userID, projectID)
	if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02")); err != nil {
		t.Fatalf("first create: %v", err)
	}

	// A second insert for the same (rule, occurrence_date) must be a no-op.
	_, err := pool.Exec(ctx, `
		INSERT INTO tasks (id, user_id, project_id, title, status, priority, energy,
			scheduled_for, display_order, recurrence_rule_id, occurrence_date)
		VALUES ($1, $2, $3, 'dup', 'active', 'medium', 'medium', '2026-03-02', 0, $4, '2026-03-02')
		ON CONFLICT (recurrence_rule_id, occurrence_date) WHERE recurrence_rule_id IS NOT NULL
		DO NOTHING`,
		uuid.NewString(), userID, projectID, rule.ID,
	)
	if err != nil {
		t.Fatalf("duplicate insert: %v", err)
	}
	if n := countTasks(t, pool, rule.ID); n != 1 {
		t.Errorf("occurrence count = %d, want 1 — the unique index did not hold", n)
	}
}

// Two rules may share an occurrence_date; the index is scoped per rule.
func TestRepo_DistinctRulesShareADate(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	for range 2 {
		rule := newRule(userID, projectID)
		if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02")); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	var n int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM tasks WHERE user_id = $1 AND occurrence_date = '2026-03-02'`, userID,
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("occurrences = %d, want 2 (one per rule)", n)
	}
}

// Deleting a rule orphans its history (ON DELETE SET NULL) and removes only the
// pending instance. Completed occurrences are the user's record of what they did.
func TestRepo_DeleteOrphansHistoryAndReapsPending(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	rule := newRule(userID, projectID)
	if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02")); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Mark the first instance done — it is history now.
	if _, err := pool.Exec(ctx,
		`UPDATE tasks SET status = 'done' WHERE recurrence_rule_id = $1`, rule.ID,
	); err != nil {
		t.Fatalf("complete first: %v", err)
	}
	// A second, still-pending instance.
	pending := newOccurrence(rule, "2026-03-09")
	if _, err := pool.Exec(ctx, `
		INSERT INTO tasks (id, user_id, project_id, title, status, priority, energy,
			scheduled_for, display_order, recurrence_rule_id, occurrence_date)
		VALUES ($1, $2, $3, 'pending', 'active', 'medium', 'medium', '2026-03-09', 1, $4, '2026-03-09')`,
		pending.ID, userID, projectID, rule.ID,
	); err != nil {
		t.Fatalf("insert pending: %v", err)
	}

	if err := r.Delete(ctx, userID, rule.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// The done instance survives, orphaned.
	var status string
	var ruleRef *string
	if err := pool.QueryRow(ctx,
		`SELECT status, recurrence_rule_id FROM tasks WHERE user_id = $1`, userID,
	).Scan(&status, &ruleRef); err != nil {
		t.Fatalf("read survivor: %v", err)
	}
	if status != "done" {
		t.Errorf("surviving task status = %q, want done", status)
	}
	if ruleRef != nil {
		t.Errorf("recurrence_rule_id = %v, want NULL (orphaned, not cascaded)", *ruleRef)
	}
	if n := countTasks(t, pool, rule.ID); n != 0 {
		t.Errorf("tasks still pointing at the rule = %d, want 0", n)
	}
}

// A rule belonging to another user is indistinguishable from a missing one.
func TestRepo_RowLevelIsolation(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	owner := seedUser(t, pool)
	intruder := seedUser(t, pool)
	projectID := seedProject(t, pool, owner)

	rule := newRule(owner, projectID)
	if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02")); err != nil {
		t.Fatalf("create: %v", err)
	}

	assertNotFound := func(t *testing.T, err error) {
		t.Helper()
		var ae *apperror.AppError
		if err == nil || !errors.As(err, &ae) || ae.Code != apperror.ErrRecurrenceRuleNotFound {
			t.Fatalf("err = %v, want %s", err, apperror.ErrRecurrenceRuleNotFound)
		}
	}

	t.Run("GetByID", func(t *testing.T) {
		_, err := r.GetByID(ctx, intruder, rule.ID)
		assertNotFound(t, err)
	})
	t.Run("SetPaused", func(t *testing.T) {
		_, err := r.SetPaused(ctx, intruder, rule.ID, true)
		assertNotFound(t, err)
	})
	t.Run("Delete", func(t *testing.T) {
		assertNotFound(t, r.Delete(ctx, intruder, rule.ID))
	})
	t.Run("List excludes other users", func(t *testing.T) {
		rules, err := r.List(ctx, intruder, nil)
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(rules) != 0 {
			t.Errorf("rules = %d, want 0", len(rules))
		}
	})

	// The owner's rule is untouched by every rejected attempt above.
	if _, err := r.GetByID(ctx, owner, rule.ID); err != nil {
		t.Errorf("owner GetByID: %v", err)
	}
}

// Update re-stamps the live instance and leaves completed history alone.
func TestRepo_UpdateRestampsLiveInstanceOnly(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	rule := newRule(userID, projectID)
	if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02")); err != nil {
		t.Fatalf("create: %v", err)
	}
	doneID := uuid.NewString()
	if _, err := pool.Exec(ctx, `
		INSERT INTO tasks (id, user_id, project_id, title, status, priority, energy,
			scheduled_for, display_order, recurrence_rule_id, occurrence_date)
		VALUES ($1, $2, $3, 'old title', 'done', 'medium', 'medium', '2026-02-23', 5, $4, '2026-02-23')`,
		doneID, userID, projectID, rule.ID,
	); err != nil {
		t.Fatalf("insert history: %v", err)
	}

	rule.Title = "Water the ferns"
	rule.Priority = "high"
	if _, err := r.Update(ctx, rule); err != nil {
		t.Fatalf("Update: %v", err)
	}

	var liveTitle string
	if err := pool.QueryRow(ctx,
		`SELECT title FROM tasks WHERE recurrence_rule_id = $1 AND status = 'active'`, rule.ID,
	).Scan(&liveTitle); err != nil {
		t.Fatalf("read live: %v", err)
	}
	if liveTitle != "Water the ferns" {
		t.Errorf("live instance title = %q, want the new template title", liveTitle)
	}

	var historyTitle string
	if err := pool.QueryRow(ctx, `SELECT title FROM tasks WHERE id = $1`, doneID).Scan(&historyTitle); err != nil {
		t.Fatalf("read history: %v", err)
	}
	if historyTitle != "old title" {
		t.Errorf("history title = %q, want it untouched", historyTitle)
	}
}

func TestRepo_ListFilterAndCount(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	p1 := seedProject(t, pool, userID)
	p2 := seedProject(t, pool, userID)

	for _, pid := range []string{p1, p1, p2} {
		rule := newRule(userID, pid)
		if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02")); err != nil {
			t.Fatalf("create: %v", err)
		}
	}

	all, err := r.List(ctx, userID, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("all rules = %d, want 3", len(all))
	}

	filtered, err := r.List(ctx, userID, &p1)
	if err != nil {
		t.Fatalf("List filtered: %v", err)
	}
	if len(filtered) != 2 {
		t.Errorf("p1 rules = %d, want 2", len(filtered))
	}

	count, err := r.CountByUser(ctx, userID)
	if err != nil {
		t.Fatalf("CountByUser: %v", err)
	}
	if count != 3 {
		t.Errorf("count = %d, want 3", count)
	}
}

func TestRepo_ProjectOwned(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	owner := seedUser(t, pool)
	intruder := seedUser(t, pool)
	projectID := seedProject(t, pool, owner)

	owned, err := r.ProjectOwned(ctx, owner, projectID)
	if err != nil || !owned {
		t.Errorf("owner ProjectOwned = %v (%v), want true", owned, err)
	}
	owned, err = r.ProjectOwned(ctx, intruder, projectID)
	if err != nil || owned {
		t.Errorf("intruder ProjectOwned = %v (%v), want false", owned, err)
	}
}

// A nullable end_date and a null cursor round-trip correctly — an exhausted
// series must not read back as "fires again".
func TestRepo_NullableCursorRoundTrips(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	rule := newRule(userID, projectID)
	rule.NextOccurrence = nil
	end := date("2026-03-02")
	rule.EndDate = &end

	created, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.NextOccurrence != nil {
		t.Errorf("nextOccurrence = %v, want nil", *created.NextOccurrence)
	}
	if created.EndDate == nil || !created.EndDate.Equal(end) {
		t.Errorf("endDate = %v, want %v", created.EndDate, end)
	}

	got, err := r.GetByID(ctx, userID, rule.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.NextOccurrence != nil {
		t.Errorf("reloaded nextOccurrence = %v, want nil", *got.NextOccurrence)
	}
}

func TestRepo_SetPausedTogglesCursorEligibility(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	rule := newRule(userID, projectID)
	if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02")); err != nil {
		t.Fatalf("create: %v", err)
	}

	paused, err := r.SetPaused(ctx, userID, rule.ID, true)
	if err != nil {
		t.Fatalf("SetPaused: %v", err)
	}
	if !paused.Paused {
		t.Error("paused = false, want true")
	}

	resumed, err := r.SetPaused(ctx, userID, rule.ID, false)
	if err != nil {
		t.Fatalf("SetPaused resume: %v", err)
	}
	if resumed.Paused {
		t.Error("paused = true, want false")
	}
}
