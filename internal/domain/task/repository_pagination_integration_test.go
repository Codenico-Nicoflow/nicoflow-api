//go:build integration

package task_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/domain/task"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
)

const paginationTestEmailDomain = "@task-pagination.test"

func cleanPaginationTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	queries := []string{
		`DELETE FROM tasks    WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%` + paginationTestEmailDomain + `')`,
		`DELETE FROM projects WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%` + paginationTestEmailDomain + `')`,
		`DELETE FROM areas    WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%` + paginationTestEmailDomain + `')`,
		`DELETE FROM users    WHERE email LIKE '%` + paginationTestEmailDomain + `'`,
	}
	for _, q := range queries {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			t.Fatalf("cleanPaginationTestData: %v", err)
		}
	}
}

// seedPaginationFixture creates one user/area/project and 15 tasks whose
// display_order, created_at, title and priority are deliberately
// uncorrelated with each other — display_order runs in the OPPOSITE order
// the rows were created in, which is exactly the scenario that exposed the
// original bug (ORDER BY display_order but seek predicate on created_at).
func seedPaginationFixture(t *testing.T, pool *pgxpool.Pool) (userID, projectID string) {
	t.Helper()
	ctx := context.Background()

	userID = uuid.New().String()
	_, err := pool.Exec(ctx,
		`INSERT INTO users (id, email, username, password_hash, plan) VALUES ($1, $2, $3, 'x', 'free')`,
		userID, "pagination-"+uuid.New().String()[:8]+paginationTestEmailDomain, uuid.New().String()[:12],
	)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}

	areaID := uuid.New().String()
	if _, err := pool.Exec(ctx,
		`INSERT INTO areas (id, user_id, name) VALUES ($1, $2, 'Pagination Area')`,
		areaID, userID,
	); err != nil {
		t.Fatalf("insert area: %v", err)
	}

	projectID = uuid.New().String()
	if _, err := pool.Exec(ctx,
		`INSERT INTO projects (id, user_id, area_id, name) VALUES ($1, $2, $3, 'Pagination Project')`,
		projectID, userID, areaID,
	); err != nil {
		t.Fatalf("insert project: %v", err)
	}

	priorities := []string{"low", "medium", "high"}
	base := time.Now().Add(-1 * time.Hour)
	for i := 0; i < 15; i++ {
		// display_order runs 14..0 (reverse of creation order); created_at
		// runs forward with creation order — the two orderings disagree on
		// every adjacent pair, which is what a matched-column keyset seek
		// must tolerate and a mismatched one cannot.
		displayOrder := 14 - i
		createdAt := base.Add(time.Duration(i) * time.Minute)
		title := "task-" + string(rune('a'+i))
		priority := priorities[i%len(priorities)]

		if _, err := pool.Exec(ctx,
			`INSERT INTO tasks (id, user_id, project_id, title, status, display_order, created_at, updated_at, priority)
			 VALUES ($1, $2, $3, $4, 'active', $5, $6, $6, $7)`,
			uuid.New().String(), userID, projectID, title, displayOrder, createdAt, priority,
		); err != nil {
			t.Fatalf("insert task %d: %v", i, err)
		}
	}

	return userID, projectID
}

// paginateAll drains every page for the given filter, asserting monotonic
// progress (no infinite loop) and returning the concatenated task IDs in
// the order the server returned them.
func paginateAll(t *testing.T, repo task.Repository, userID, projectID string, f task.ListTasksFilter) []string {
	t.Helper()
	var ids []string
	seenCursors := map[string]bool{}
	cursor := ""
	for page := 0; page < 20; page++ { // hard cap — a real bug loops forever otherwise
		f.Cursor = cursor
		f.Limit = 5
		tasks, next, err := repo.ListByProject(context.Background(), userID, projectID, f)
		if err != nil {
			t.Fatalf("ListByProject page %d: %v", page, err)
		}
		for _, tk := range tasks {
			ids = append(ids, tk.ID)
		}
		if next == "" {
			return ids
		}
		if seenCursors[next] {
			t.Fatalf("cursor repeated — pagination looping: %q", next)
		}
		seenCursors[next] = true
		cursor = next
	}
	t.Fatal("paginateAll: exceeded 20 pages without terminating")
	return nil
}

func assertNoDuplicatesAndCount(t *testing.T, ids []string, want int) {
	t.Helper()
	if len(ids) != want {
		t.Errorf("got %d task ids across all pages, want %d (dupes/gaps): %v", len(ids), want, ids)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Errorf("duplicate task id across pages: %s", id)
		}
		seen[id] = true
	}
}

func TestListByProject_Pagination_DefaultDisplayOrderSort(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanPaginationTestData(t, pool)
	t.Cleanup(func() { cleanPaginationTestData(t, pool) })

	userID, projectID := seedPaginationFixture(t, pool)
	repo := task.NewRepository(pool)

	// No sortField sent — this is the exact path the frontend's infinite
	// query always takes (TasksSection never sets sortField), and the path
	// that was broken: rows are ORDER BY display_order but the old seek
	// predicate filtered on the unrelated created_at/id tuple.
	ids := paginateAll(t, repo, userID, projectID, task.ListTasksFilter{})
	assertNoDuplicatesAndCount(t, ids, 15)
}

func TestListByProject_Pagination_TitleSortTextCodec(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanPaginationTestData(t, pool)
	t.Cleanup(func() { cleanPaginationTestData(t, pool) })

	userID, projectID := seedPaginationFixture(t, pool)
	repo := task.NewRepository(pool)

	ids := paginateAll(t, repo, userID, projectID, task.ListTasksFilter{SortField: "title", SortOrder: "asc"})
	assertNoDuplicatesAndCount(t, ids, 15)
}

func TestListByProject_Pagination_PrioritySortWithTies(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanPaginationTestData(t, pool)
	t.Cleanup(func() { cleanPaginationTestData(t, pool) })

	userID, projectID := seedPaginationFixture(t, pool)
	repo := task.NewRepository(pool)

	// priority repeats every 3 tasks — exercises the id tie-breaker.
	ids := paginateAll(t, repo, userID, projectID, task.ListTasksFilter{SortField: "priority", SortOrder: "desc"})
	assertNoDuplicatesAndCount(t, ids, 15)
}

func TestListByProject_Pagination_CreatedAtSortRegression(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanPaginationTestData(t, pool)
	t.Cleanup(func() { cleanPaginationTestData(t, pool) })

	userID, projectID := seedPaginationFixture(t, pool)
	repo := task.NewRepository(pool)

	// The one sort path that already worked before the fix — guard against
	// a regression in the time codec.
	ids := paginateAll(t, repo, userID, projectID, task.ListTasksFilter{SortField: "createdAt", SortOrder: "desc"})
	assertNoDuplicatesAndCount(t, ids, 15)
}

func TestListByProject_Pagination_RowIsolationAcrossUsers(t *testing.T) {
	pool := testutil.NewTestDB(t)
	cleanPaginationTestData(t, pool)
	t.Cleanup(func() { cleanPaginationTestData(t, pool) })

	userA, projectA := seedPaginationFixture(t, pool)
	_, projectB := seedPaginationFixture(t, pool)
	repo := task.NewRepository(pool)

	ids := paginateAll(t, repo, userA, projectA, task.ListTasksFilter{})
	assertNoDuplicatesAndCount(t, ids, 15)

	// userA's cursor/pagination over projectA must never surface projectB's rows.
	for _, id := range ids {
		if id == projectB {
			t.Fatalf("task from another user's project leaked: %s", id)
		}
	}
}
