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

// seedPlainTask inserts an ordinary, non-recurring task — the row a
// convert-to-recurring call targets — with a caller-chosen scheduled_for so
// tests can exercise the "start date doesn't itself satisfy the schedule"
// case where the first real occurrence lands on a different date.
func seedPlainTask(t *testing.T, pool *pgxpool.Pool, userID, projectID, scheduledFor string) string {
	t.Helper()
	taskID := uuid.NewString()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO tasks (id, user_id, project_id, title, status, priority, energy, scheduled_for, display_order)
		VALUES ($1, $2, $3, 'Wash the floors', 'active', 'high', 'low', $4, 0)`,
		taskID, userID, projectID, scheduledFor,
	); err != nil {
		t.Fatalf("seedPlainTask: %v", err)
	}
	return taskID
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
	got, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02"), 0)
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

// freeLimit makes the insert conditional on the user's live rule count, so a
// 4th create on a 3-rule free plan is rejected — and rejected atomically: the
// count-check and the insert happen under the same advisory lock, closing the
// TOCTOU window a plain "count then insert" would leave open between two
// concurrent requests.
func TestRepo_CreateWithOccurrence_FreeLimit(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	dates := []string{"2026-03-02", "2026-03-09", "2026-03-16", "2026-03-23"}
	for i := 0; i < 3; i++ {
		rule := newRule(userID, projectID)
		if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, dates[i]), 3); err != nil {
			t.Fatalf("create rule %d: %v", i, err)
		}
	}

	fourth := newRule(userID, projectID)
	_, err := r.CreateWithOccurrence(ctx, fourth, newOccurrence(fourth, dates[3]), 3)
	if err == nil {
		t.Fatal("4th create under a limit of 3 succeeded, want PLAN_LIMIT_EXCEEDED")
	}
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Code != apperror.ErrPlanLimitExceeded {
		t.Errorf("err = %v, want PLAN_LIMIT_EXCEEDED", err)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM recurrence_rules WHERE user_id = $1`, userID,
	).Scan(&count); err != nil {
		t.Fatalf("count rules: %v", err)
	}
	if count != 3 {
		t.Errorf("rule count = %d, want 3 (rejected insert must not land a row)", count)
	}
}

// Converting a plain task attaches a brand-new rule to the SAME row — no new
// task is inserted — and forces it back to active.
func TestRepo_ConvertTask(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)
	taskID := seedPlainTask(t, pool, userID, projectID, "2026-03-09") // a Monday, matches the rule below

	rule := newRule(userID, projectID)
	got, err := r.ConvertTask(ctx, taskID, rule, date("2026-03-09"), 0)
	if err != nil {
		t.Fatalf("ConvertTask: %v", err)
	}
	if got.ID != rule.ID {
		t.Errorf("rule.ID = %s, want %s", got.ID, rule.ID)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE user_id = $1`, userID).Scan(&count); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if count != 1 {
		t.Errorf("task count = %d, want 1 — convert must reuse the existing row, never insert a new one", count)
	}

	var status, scheduledFor, ruleID string
	if err := pool.QueryRow(ctx,
		`SELECT status, scheduled_for, recurrence_rule_id FROM tasks WHERE id = $1`, taskID,
	).Scan(&status, &scheduledFor, &ruleID); err != nil {
		t.Fatalf("read converted task: %v", err)
	}
	if status != "active" {
		t.Errorf("status = %q, want active (forced on convert)", status)
	}
	if scheduledFor != "2026-03-09" {
		t.Errorf("scheduledFor = %q, want 2026-03-09", scheduledFor)
	}
	if ruleID != rule.ID {
		t.Errorf("recurrence_rule_id = %q, want %s", ruleID, rule.ID)
	}
}

// A task already governed by a rule must reject a second convert rather than
// silently re-parenting it — that would orphan the first rule's cursor.
func TestRepo_ConvertTask_RejectsAlreadyRecurring(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	firstRule := newRule(userID, projectID)
	firstOcc := newOccurrence(firstRule, "2026-03-02")
	if _, err := r.CreateWithOccurrence(ctx, firstRule, firstOcc, 0); err != nil {
		t.Fatalf("seed first rule: %v", err)
	}

	secondRule := newRule(userID, projectID)
	_, err := r.ConvertTask(ctx, firstOcc.ID, secondRule, date("2026-03-09"), 0)
	if err == nil {
		t.Fatal("convert on an already-recurring task succeeded, want TASK_ALREADY_RECURRING")
	}
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Code != apperror.ErrTaskAlreadyRecurring {
		t.Errorf("err = %v, want TASK_ALREADY_RECURRING", err)
	}

	// The rejected convert must not have landed the second rule row either —
	// the whole attempt rolls back together.
	var count int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM recurrence_rules WHERE id = $1`, secondRule.ID,
	).Scan(&count); err != nil {
		t.Fatalf("count rules: %v", err)
	}
	if count != 0 {
		t.Error("second rule was inserted despite the rejected convert — the transaction did not roll back")
	}
}

// Convert shares the same free-plan guard as CreateWithOccurrence.
func TestRepo_ConvertTask_FreeLimit(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	dates := []string{"2026-03-02", "2026-03-09", "2026-03-16"}
	for i := 0; i < 3; i++ {
		rule := newRule(userID, projectID)
		if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, dates[i]), 3); err != nil {
			t.Fatalf("seed rule %d: %v", i, err)
		}
	}

	taskID := seedPlainTask(t, pool, userID, projectID, "2026-03-23")
	fourth := newRule(userID, projectID)
	_, err := r.ConvertTask(ctx, taskID, fourth, date("2026-03-23"), 3)
	if err == nil {
		t.Fatal("convert under a limit of 3 succeeded, want PLAN_LIMIT_EXCEEDED")
	}
	var ae *apperror.AppError
	if !errors.As(err, &ae) || ae.Code != apperror.ErrPlanLimitExceeded {
		t.Errorf("err = %v, want PLAN_LIMIT_EXCEEDED", err)
	}

	// The task must be left exactly as it was — still a plain task.
	var ruleID *string
	if err := pool.QueryRow(ctx,
		`SELECT recurrence_rule_id FROM tasks WHERE id = $1`, taskID,
	).Scan(&ruleID); err != nil {
		t.Fatalf("read task: %v", err)
	}
	if ruleID != nil {
		t.Error("task gained a recurrence_rule_id despite the rejected convert")
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
	if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02"), 0); err != nil {
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
		if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02"), 0); err != nil {
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
	if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02"), 0); err != nil {
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
	if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02"), 0); err != nil {
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
	if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02"), 0); err != nil {
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
		if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02"), 0); err != nil {
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

	created, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02"), 0)
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
	if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02"), 0); err != nil {
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

// ── materializer (NIC-1773) ──────────────────────────────────────────────────

func seedUserTZ(t *testing.T, pool *pgxpool.Pool, tz string) string {
	t.Helper()
	id := seedUser(t, pool)
	if _, err := pool.Exec(context.Background(),
		`UPDATE users SET timezone = $2 WHERE id = $1`, id, tz,
	); err != nil {
		t.Fatalf("seedUserTZ: %v", err)
	}
	return id
}

// taskStatus returns the occurrence's effective status the way the streak
// calculator sees it: COALESCE(occurrence_status, status), so a reaped/missed
// occurrence reads as "missed" even though its real status column is
// 'cancelled'.
func taskStatus(t *testing.T, pool *pgxpool.Pool, ruleID, occDate string) string {
	t.Helper()
	var s string
	if err := pool.QueryRow(context.Background(),
		`SELECT COALESCE(occurrence_status, status) FROM tasks WHERE recurrence_rule_id = $1 AND occurrence_date = $2`,
		ruleID, occDate,
	).Scan(&s); err != nil {
		t.Fatalf("taskStatus(%s): %v", occDate, err)
	}
	return s
}

func ruleCursor(t *testing.T, pool *pgxpool.Pool, ruleID string) *string {
	t.Helper()
	var c *string
	if err := pool.QueryRow(context.Background(),
		`SELECT next_occurrence::text FROM recurrence_rules WHERE id = $1`, ruleID,
	).Scan(&c); err != nil {
		t.Fatalf("ruleCursor: %v", err)
	}
	return c
}

// Materialize inserts the next occurrence, reaps the lapsed one to `missed`, and
// advances the cursor — all together.
func TestRepo_MaterializeReapsAndAdvances(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	rule := newRule(userID, projectID)
	if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02"), 0); err != nil {
		t.Fatalf("create: %v", err)
	}

	advanced := rule
	next := date("2026-03-16")
	advanced.NextOccurrence = &next
	occ := newOccurrence(rule, "2026-03-09")

	out, err := r.Materialize(ctx, advanced, occ, 50)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if out.Created == nil {
		t.Fatal("Created = nil, want the new occurrence")
	}
	if !out.Reaped {
		t.Error("Reaped = false, want the lapsed instance reaped")
	}
	if got := taskStatus(t, pool, rule.ID, "2026-03-02"); got != "missed" {
		t.Errorf("lapsed instance status = %q, want missed", got)
	}
	if got := taskStatus(t, pool, rule.ID, "2026-03-09"); got != "active" {
		t.Errorf("new instance status = %q, want active", got)
	}
	if got := ruleCursor(t, pool, rule.ID); got == nil || *got != "2026-03-16" {
		t.Errorf("cursor = %v, want 2026-03-16", got)
	}
}

// The reap sets `missed`, never `cancelled`, and never touches a done instance —
// the streak calculation depends on telling them apart.
func TestRepo_MaterializeDoesNotReapCompleted(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	rule := newRule(userID, projectID)
	if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02"), 0); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := pool.Exec(ctx,
		`UPDATE tasks SET status = 'done' WHERE recurrence_rule_id = $1`, rule.ID,
	); err != nil {
		t.Fatalf("complete: %v", err)
	}

	advanced := rule
	next := date("2026-03-16")
	advanced.NextOccurrence = &next
	out, err := r.Materialize(ctx, advanced, newOccurrence(rule, "2026-03-09"), 50)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if out.Reaped {
		t.Error("Reaped = true, want a completed instance left alone")
	}
	if got := taskStatus(t, pool, rule.ID, "2026-03-02"); got != "done" {
		t.Errorf("completed instance status = %q, want it untouched at done", got)
	}
}

// Running twice creates no duplicate — the partial unique index is what lets the
// cron sweep and the sync-on-complete path race safely.
func TestRepo_MaterializeIsIdempotent(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	rule := newRule(userID, projectID)
	if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02"), 0); err != nil {
		t.Fatalf("create: %v", err)
	}

	advanced := rule
	next := date("2026-03-16")
	advanced.NextOccurrence = &next
	occ := newOccurrence(rule, "2026-03-09")

	if _, err := r.Materialize(ctx, advanced, occ, 50); err != nil {
		t.Fatalf("first Materialize: %v", err)
	}
	// A second attempt for the same date — the horizon is already satisfied.
	second, err := r.Materialize(ctx, advanced, newOccurrence(rule, "2026-03-09"), 50)
	if err != nil {
		t.Fatalf("second Materialize: %v", err)
	}
	if second.Created != nil {
		t.Error("second run created a row, want the duplicate suppressed")
	}
	if n := countTasks(t, pool, rule.ID); n != 2 {
		t.Errorf("occurrence rows = %d, want 2 (the original + one new)", n)
	}
}

// The horizon is exactly one live instance per rule: a still-open instance for a
// later date blocks materialization rather than stacking up.
func TestRepo_MaterializeRespectsHorizon(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	rule := newRule(userID, projectID)
	if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-09"), 0); err != nil {
		t.Fatalf("create: %v", err)
	}

	advanced := rule
	next := date("2026-03-16")
	advanced.NextOccurrence = &next
	// An earlier date while a later instance is still live.
	out, err := r.Materialize(ctx, advanced, newOccurrence(rule, "2026-03-02"), 50)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if !out.SkippedExisting {
		t.Error("SkippedExisting = false, want the live instance to hold the horizon")
	}
	if n := countTasks(t, pool, rule.ID); n != 1 {
		t.Errorf("occurrence rows = %d, want 1", n)
	}
}

// The plan-limit stall: at the cap, nothing is inserted AND the cursor stays put
// so the occurrence is retried rather than silently skipped.
func TestRepo_MaterializePlanLimitLeavesCursorUnchanged(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	rule := newRule(userID, projectID)
	if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02"), 0); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Complete it so the horizon is free, leaving the project's one active task
	// as the entire budget.
	if _, err := pool.Exec(ctx,
		`UPDATE tasks SET status = 'done' WHERE recurrence_rule_id = $1`, rule.ID,
	); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tasks (id, user_id, project_id, title, status, priority, energy, display_order)
		VALUES ($1, $2, $3, 'filler', 'active', 'medium', 'medium', 9)`,
		uuid.NewString(), userID, projectID,
	); err != nil {
		t.Fatalf("filler: %v", err)
	}

	before := ruleCursor(t, pool, rule.ID)

	advanced := rule
	next := date("2026-03-16")
	advanced.NextOccurrence = &next
	// limit=1 and the project already holds 1 active task.
	out, err := r.Materialize(ctx, advanced, newOccurrence(rule, "2026-03-09"), 1)
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if !out.SkippedPlanLimit {
		t.Fatal("SkippedPlanLimit = false, want the guard to trip")
	}
	after := ruleCursor(t, pool, rule.ID)
	if (before == nil) != (after == nil) || (before != nil && *before != *after) {
		t.Errorf("cursor moved from %v to %v — the occurrence would be lost", before, after)
	}
	if n := countTasks(t, pool, rule.ID); n != 1 {
		t.Errorf("occurrence rows = %d, want 1 (nothing new inserted)", n)
	}
}

// ListDue returns non-paused, non-exhausted, due rules with the owner's zone.
func TestRepo_ListDue(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUserTZ(t, pool, "Asia/Jerusalem")
	projectID := seedProject(t, pool, userID)

	mk := func(next *time.Time, paused bool) string {
		rule := newRule(userID, projectID)
		rule.NextOccurrence = next
		rule.Paused = paused
		created, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02"), 0)
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if paused {
			if _, err := r.SetPaused(ctx, userID, created.ID, true); err != nil {
				t.Fatalf("pause: %v", err)
			}
		}
		return created.ID
	}

	past := date("2020-01-01")
	future := date("2999-01-01")
	dueID := mk(&past, false)
	mk(&past, true)    // paused → excluded
	mk(nil, false)     // exhausted → excluded
	mk(&future, false) // far future → excluded by the SQL bound

	due, err := r.ListDue(ctx)
	if err != nil {
		t.Fatalf("ListDue: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("due rules = %d, want 1", len(due))
	}
	if due[0].Rule.ID != dueID {
		t.Errorf("due rule = %s, want %s", due[0].Rule.ID, dueID)
	}
	if due[0].Timezone != "Asia/Jerusalem" {
		t.Errorf("timezone = %q, want Asia/Jerusalem", due[0].Timezone)
	}
}

// Stats are derived from occurrence rows; the streak walks back from newest.
func TestRepo_OccurrenceStatsAndStreak(t *testing.T) {
	r, pool := newRepo(t)
	ctx := context.Background()
	userID := seedUser(t, pool)
	projectID := seedProject(t, pool, userID)

	rule := newRule(userID, projectID)
	if _, err := r.CreateWithOccurrence(ctx, rule, newOccurrence(rule, "2026-03-02"), 0); err != nil {
		t.Fatalf("create: %v", err)
	}
	// add seeds an occurrence row with the given *effective* status (what the
	// streak calculator sees). "missed" is synthetic — never a real status
	// value — so it lands in occurrence_status with status='cancelled',
	// matching what Materialize's reap actually writes.
	add := func(d, status string) {
		realStatus, occStatus := status, (*string)(nil)
		if status == "missed" {
			realStatus = "cancelled"
			m := "missed"
			occStatus = &m
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO tasks (id, user_id, project_id, title, status, occurrence_status,
				priority, energy, display_order, recurrence_rule_id, occurrence_date)
			VALUES ($1, $2, $3, 'occ', $4, $5, 'medium', 'medium', 0, $6, $7::date)`,
			uuid.NewString(), userID, projectID, realStatus, occStatus, rule.ID, d,
		); err != nil {
			t.Fatalf("add %s: %v", d, err)
		}
	}
	// Oldest → newest: done, missed, done, done. The first row (03-02) is active.
	if _, err := pool.Exec(ctx,
		`UPDATE tasks SET status = 'done' WHERE recurrence_rule_id = $1`, rule.ID,
	); err != nil {
		t.Fatalf("seed first done: %v", err)
	}
	add("2026-03-09", "missed")
	add("2026-03-16", "done")
	add("2026-03-23", "done")

	counts, err := r.CountOccurrencesByStatus(ctx, userID, rule.ID)
	if err != nil {
		t.Fatalf("CountOccurrencesByStatus: %v", err)
	}
	if counts["done"] != 3 || counts["missed"] != 1 {
		t.Errorf("counts = %v, want 3 done / 1 missed", counts)
	}

	statuses, err := r.ListOccurrenceStatuses(ctx, userID, rule.ID)
	if err != nil {
		t.Fatalf("ListOccurrenceStatuses: %v", err)
	}
	if len(statuses) != 4 || statuses[0] != "done" {
		t.Fatalf("statuses = %v, want newest-first starting with done", statuses)
	}
	// done, done, then missed breaks it.
	if got := recurrence.CurrentStreakForTest(statuses); got != 2 {
		t.Errorf("streak = %d, want 2", got)
	}
}
