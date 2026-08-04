//go:build integration

package search_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/internal/config"
	"github.com/nicoflow/nicoflow-api/internal/domain/search"
	"github.com/nicoflow/nicoflow-api/internal/handler"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
	"github.com/nicoflow/nicoflow-api/pkg/jwtutil"
)

const (
	testJWTSecret = "integration-test-secret-32-bytes!!"
	testJWTExpiry = 15 * time.Minute
)

// cleanSearchTestData removes rows belonging to search-integration users
// (email domain "@searchtest.test") to avoid cross-package races.
func cleanSearchTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	queries := []string{
		`DELETE FROM notes    WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@searchtest.test')`,
		`DELETE FROM tasks    WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@searchtest.test')`,
		`DELETE FROM projects WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@searchtest.test')`,
		`DELETE FROM areas    WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@searchtest.test')`,
		`DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@searchtest.test')`,
		`DELETE FROM users WHERE email LIKE '%@searchtest.test'`,
	}
	for _, q := range queries {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			t.Fatalf("cleanSearchTestData: %v", err)
		}
	}
}

func newSearchServer(t *testing.T) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewTestDB(t)
	cleanSearchTestData(t, pool)
	t.Cleanup(func() { cleanSearchTestData(t, pool) })

	cfg := config.Config{
		JWTSecret:          testJWTSecret,
		JWTExpiry:          testJWTExpiry,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	}

	searchSvc := search.NewService(search.NewRepository(pool))
	h := handler.Handlers{
		Search: search.NewHandler(searchSvc),
	}

	srv := httptest.NewServer(handler.New(cfg, pool, h))
	t.Cleanup(srv.Close)
	return srv, pool
}

func mustCreateUser(t *testing.T, pool *pgxpool.Pool) (userID, token string) {
	t.Helper()
	userID = uuid.New().String()
	email := fmt.Sprintf("%s@searchtest.test", userID[:8])
	_, err := pool.Exec(context.Background(), `
		INSERT INTO users (id, email, username, password_hash, plan)
		VALUES ($1, $2, $3, 'x', 'free')`,
		userID, email, userID[:8],
	)
	if err != nil {
		t.Fatalf("mustCreateUser: %v", err)
	}
	tok, err := jwtutil.Issue(userID, email, "free", testJWTSecret, testJWTExpiry)
	if err != nil {
		t.Fatalf("mustCreateUser: issue jwt: %v", err)
	}
	return userID, tok
}

// seedAreaProjectTask inserts an area → project → task chain owned by userID.
func seedAreaProjectTask(t *testing.T, pool *pgxpool.Pool, userID, areaName, projectName, taskTitle, taskNotes string) (areaID, projectID, taskID string) {
	t.Helper()
	areaID = uuid.New().String()
	projectID = uuid.New().String()
	taskID = uuid.New().String()
	ctx := context.Background()

	if _, err := pool.Exec(ctx, `
		INSERT INTO areas (id, user_id, name, color, icon)
		VALUES ($1, $2, $3, '#3B82F6', 'folder')`,
		areaID, userID, areaName,
	); err != nil {
		t.Fatalf("seed area: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO projects (id, user_id, area_id, name, status, folder_icon)
		VALUES ($1, $2, $3, $4, 'active', 'folder')`,
		projectID, userID, areaID, projectName,
	); err != nil {
		t.Fatalf("seed project: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO tasks (id, user_id, project_id, title, notes, status)
		VALUES ($1, $2, $3, $4, $5, 'inbox')`,
		taskID, userID, projectID, taskTitle, taskNotes,
	); err != nil {
		t.Fatalf("seed task: %v", err)
	}
	return areaID, projectID, taskID
}

func doGet(t *testing.T, url, token string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

type searchEnvelope struct {
	Data  search.Response `json:"data"`
	Error *struct {
		Code string `json:"code"`
	} `json:"error"`
}

func TestIntegration_Search_GroupsResultsAndIsolatesByUser(t *testing.T) {
	srv, pool := newSearchServer(t)
	userA, tokenA := mustCreateUser(t, pool)
	userB, _ := mustCreateUser(t, pool)

	seedAreaProjectTask(t, pool, userA, "Home", "Bucket cleanup", "Ship the bucket page", "finalize inbox flow")
	// User B has a matching row that must never appear in A's results.
	seedAreaProjectTask(t, pool, userB, "Bucket zone", "Bucket revamp", "Bucket redesign", "")

	resp := doGet(t, srv.URL+"/v1/search?q=bucket", tokenA)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var env searchEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error != nil {
		t.Fatalf("unexpected error: %+v", env.Error)
	}

	// Task hit: A's "Ship the bucket page" grouped under tasks with its project.
	if len(env.Data.Tasks) != 1 {
		t.Fatalf("tasks: want 1, got %d (%+v)", len(env.Data.Tasks), env.Data.Tasks)
	}
	task := env.Data.Tasks[0]
	if task.Title != "Ship the bucket page" {
		t.Errorf("task title: got %q", task.Title)
	}
	if task.ProjectName != "Bucket cleanup" {
		t.Errorf("task projectName: want %q, got %q", "Bucket cleanup", task.ProjectName)
	}
	if task.ProjectID == "" {
		t.Error("task projectId should be non-empty")
	}

	// Project hit: "Bucket cleanup" grouped under projects with its area.
	if len(env.Data.Projects) != 1 {
		t.Fatalf("projects: want 1, got %d (%+v)", len(env.Data.Projects), env.Data.Projects)
	}
	if env.Data.Projects[0].Name != "Bucket cleanup" {
		t.Errorf("project name: got %q", env.Data.Projects[0].Name)
	}
	if env.Data.Projects[0].AreaName != "Home" {
		t.Errorf("project areaName: want %q, got %q", "Home", env.Data.Projects[0].AreaName)
	}

	// Area hit: A has no area matching "bucket" → empty group (B's "Bucket zone" excluded).
	if len(env.Data.Areas) != 0 {
		t.Errorf("areas: want 0 for user A, got %d (%+v)", len(env.Data.Areas), env.Data.Areas)
	}

	// Ownership isolation: none of B's rows leak into any group.
	for _, tk := range env.Data.Tasks {
		if tk.Title == "Bucket redesign" {
			t.Error("user B's task leaked into user A's results")
		}
	}
	for _, p := range env.Data.Projects {
		if p.Name == "Bucket revamp" {
			t.Error("user B's project leaked into user A's results")
		}
	}
	for _, a := range env.Data.Areas {
		if a.Name == "Bucket zone" {
			t.Error("user B's area leaked into user A's results")
		}
	}
}

func TestIntegration_Search_TypesFilter(t *testing.T) {
	srv, pool := newSearchServer(t)
	userA, tokenA := mustCreateUser(t, pool)
	seedAreaProjectTask(t, pool, userA, "Bucket area", "Bucket cleanup", "Ship the bucket page", "")

	resp := doGet(t, srv.URL+"/v1/search?q=bucket&types=task", tokenA)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var env searchEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data.Tasks) != 1 {
		t.Errorf("tasks: want 1, got %d", len(env.Data.Tasks))
	}
	if len(env.Data.Projects) != 0 {
		t.Errorf("projects: want 0 (not requested), got %d", len(env.Data.Projects))
	}
	if len(env.Data.Areas) != 0 {
		t.Errorf("areas: want 0 (not requested), got %d", len(env.Data.Areas))
	}
}

func TestIntegration_Search_ShortQuery_Returns400(t *testing.T) {
	srv, pool := newSearchServer(t)
	_, tokenA := mustCreateUser(t, pool)

	resp := doGet(t, srv.URL+"/v1/search?q=a", tokenA)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", resp.StatusCode)
	}
	var env searchEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error == nil || env.Error.Code != apperror.ErrInvalidInput {
		t.Fatalf("want INVALID_INPUT, got %+v", env.Error)
	}
}

func TestIntegration_Search_NoJWT_Returns401(t *testing.T) {
	srv, pool := newSearchServer(t)
	_ = pool

	resp := doGet(t, srv.URL+"/v1/search?q=bucket", "" /* no token */)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", resp.StatusCode)
	}
	var env searchEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error == nil || env.Error.Code != apperror.ErrUnauthorized {
		t.Fatalf("want UNAUTHORIZED, got %+v", env.Error)
	}
}

// A partial term must prefix-match the full word: "testin" finds "testing".
func TestIntegration_Search_PrefixMatch(t *testing.T) {
	srv, pool := newSearchServer(t)
	userA, tokenA := mustCreateUser(t, pool)

	seedAreaProjectTask(t, pool, userA, "Testing area", "Testing project", "Testing task", "")

	resp := doGet(t, srv.URL+"/v1/search?q=testin", tokenA)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var env searchEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error != nil {
		t.Fatalf("unexpected error: %+v", env.Error)
	}
	if len(env.Data.Tasks) != 1 || env.Data.Tasks[0].Title != "Testing task" {
		t.Errorf("prefix 'testin' should match task 'Testing task', got %+v", env.Data.Tasks)
	}
	if len(env.Data.Projects) != 1 || env.Data.Projects[0].Name != "Testing project" {
		t.Errorf("prefix 'testin' should match 'Testing project', got %+v", env.Data.Projects)
	}
	if len(env.Data.Areas) != 1 || env.Data.Areas[0].Name != "Testing area" {
		t.Errorf("prefix 'testin' should match 'Testing area', got %+v", env.Data.Areas)
	}
}

// ── notes in search (E-053 / NIC-1909) ───────────────────────────────────────

// seedNote inserts a note directly. projectID empty ⇒ an orphaned note, the
// state a project delete leaves behind.
func seedNote(t *testing.T, pool *pgxpool.Pool, userID, projectID, title, contentText string) string {
	t.Helper()
	id := uuid.New().String()
	var pid *string
	if projectID != "" {
		pid = &projectID
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO notes (id, user_id, project_id, title, content_text)
		VALUES ($1, $2, $3, $4, $5)`,
		id, userID, pid, title, contentText,
	); err != nil {
		t.Fatalf("seed note: %v", err)
	}
	return id
}

// AC1 — a note is findable by its title AND by its body text, because the
// search vector is generated from title || content_text.
func TestIntegration_Search_NotesMatchTitleAndBody(t *testing.T) {
	srv, pool := newSearchServer(t)
	userID, token := mustCreateUser(t, pool)
	_, projectID, _ := seedAreaProjectTask(t, pool, userID, "Work", "Reference", "unrelated task", "")
	noteID := seedNote(t, pool, userID, projectID, "GTD structure thread", "the weekly review is the keystone habit")

	for _, term := range []string{"structure", "review"} {
		t.Run(term, func(t *testing.T) {
			resp := doGet(t, srv.URL+"/v1/search?q="+term, token)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want 200", resp.StatusCode)
			}

			var env searchEnvelope
			if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if len(env.Data.Notes) != 1 {
				t.Fatalf("notes = %d, want 1 for %q", len(env.Data.Notes), term)
			}
			got := env.Data.Notes[0]
			if got.ID != noteID {
				t.Errorf("id = %q, want %q", got.ID, noteID)
			}
			if got.ProjectName != "Reference" {
				t.Errorf("projectName = %q, want the parent project's name", got.ProjectName)
			}
			if got.Excerpt == "" {
				t.Error("excerpt is empty")
			}
		})
	}
}

// AC2 — deleting a project orphans its notes rather than destroying them, and
// search is user-scoped, so an orphan stays findable. Search is the only surface
// that can still reach it.
func TestIntegration_Search_OrphanedNoteStillFound(t *testing.T) {
	srv, pool := newSearchServer(t)
	userID, token := mustCreateUser(t, pool)
	_, projectID, _ := seedAreaProjectTask(t, pool, userID, "Work", "Doomed", "unrelated task", "")
	noteID := seedNote(t, pool, userID, projectID, "orphan candidate", "kryptonite reference material")

	if _, err := pool.Exec(context.Background(), `DELETE FROM projects WHERE id = $1`, projectID); err != nil {
		t.Fatalf("delete project: %v", err)
	}

	resp := doGet(t, srv.URL+"/v1/search?q=kryptonite", token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var env searchEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data.Notes) != 1 {
		t.Fatalf("notes = %d, want the orphan still returned", len(env.Data.Notes))
	}
	got := env.Data.Notes[0]
	if got.ID != noteID {
		t.Errorf("id = %q, want %q", got.ID, noteID)
	}
	if got.ProjectID != "" || got.ProjectName != "" {
		t.Errorf("orphan carries project fields: id=%q name=%q", got.ProjectID, got.ProjectName)
	}
}

// AC3 — another user's note never appears in this user's results.
func TestIntegration_Search_NotesIsolatedByUser(t *testing.T) {
	srv, pool := newSearchServer(t)
	userA, tokenA := mustCreateUser(t, pool)
	userB, _ := mustCreateUser(t, pool)

	_, projectA, _ := seedAreaProjectTask(t, pool, userA, "A area", "A project", "a task", "")
	_, projectB, _ := seedAreaProjectTask(t, pool, userB, "B area", "B project", "b task", "")
	seedNote(t, pool, userA, projectA, "shared keyword mine", "zebra")
	seedNote(t, pool, userB, projectB, "shared keyword theirs", "zebra")

	resp := doGet(t, srv.URL+"/v1/search?q=zebra", tokenA)
	defer resp.Body.Close()

	var env searchEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data.Notes) != 1 {
		t.Fatalf("notes = %d, want only the caller's own", len(env.Data.Notes))
	}
	if env.Data.Notes[0].Title != "shared keyword mine" {
		t.Errorf("title = %q — another user's note leaked", env.Data.Notes[0].Title)
	}
}

// AC4 — types=note narrows the response to notes alone, over real HTTP.
func TestIntegration_Search_TypesNoteFilter(t *testing.T) {
	srv, pool := newSearchServer(t)
	userID, token := mustCreateUser(t, pool)
	_, projectID, _ := seedAreaProjectTask(t, pool, userID, "Zebra area", "Zebra project", "Zebra task", "")
	seedNote(t, pool, userID, projectID, "Zebra note", "zebra body")

	resp := doGet(t, srv.URL+"/v1/search?q=zebra&types=note", token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var env searchEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data.Notes) != 1 {
		t.Errorf("notes = %d, want 1", len(env.Data.Notes))
	}
	if len(env.Data.Tasks) != 0 || len(env.Data.Projects) != 0 || len(env.Data.Areas) != 0 {
		t.Errorf("other groups populated: tasks=%d projects=%d areas=%d",
			len(env.Data.Tasks), len(env.Data.Projects), len(env.Data.Areas))
	}
}

// The per-type limit applies to notes as it does to every other group.
func TestIntegration_Search_NotesRespectLimit(t *testing.T) {
	srv, pool := newSearchServer(t)
	userID, token := mustCreateUser(t, pool)
	_, projectID, _ := seedAreaProjectTask(t, pool, userID, "Work", "Bulk", "unrelated", "")
	for i := range 5 {
		seedNote(t, pool, userID, projectID, fmt.Sprintf("bulk note %d", i), "walrus")
	}

	resp := doGet(t, srv.URL+"/v1/search?q=walrus&types=note&limit=2", token)
	defer resp.Body.Close()

	var env searchEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data.Notes) != 2 {
		t.Errorf("notes = %d, want the limit of 2 respected", len(env.Data.Notes))
	}
}
