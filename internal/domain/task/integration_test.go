//go:build integration

package task_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/nicoflow/nicoflow-api/internal/config"
	"github.com/nicoflow/nicoflow-api/internal/domain/ai"
	"github.com/nicoflow/nicoflow-api/internal/domain/area"
	"github.com/nicoflow/nicoflow-api/internal/domain/auth"
	"github.com/nicoflow/nicoflow-api/internal/domain/billing"
	"github.com/nicoflow/nicoflow-api/internal/domain/bucket"
	"github.com/nicoflow/nicoflow-api/internal/domain/project"
	"github.com/nicoflow/nicoflow-api/internal/domain/task"
	"github.com/nicoflow/nicoflow-api/internal/handler"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
	"github.com/nicoflow/nicoflow-api/pkg/jwtutil"
)

const (
	integrationJWTSecret = "integration-test-secret-32-bytes!!"
	testEmailDomain      = "@task-integration.test"
)

type taskEnv struct {
	srv       *httptest.Server
	token     string
	userID    string
	projectID string
}

func cleanTaskTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	queries := []string{
		`DELETE FROM subtasks WHERE task_id IN (
			SELECT t.id FROM tasks t JOIN users u ON u.id = t.user_id
			WHERE u.email LIKE '%` + testEmailDomain + `')`,
		`DELETE FROM tasks    WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%` + testEmailDomain + `')`,
		`DELETE FROM projects WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%` + testEmailDomain + `')`,
		`DELETE FROM areas    WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%` + testEmailDomain + `')`,
		`DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%` + testEmailDomain + `')`,
		`DELETE FROM users WHERE email LIKE '%` + testEmailDomain + `'`,
	}
	for _, q := range queries {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			t.Fatalf("cleanTaskTestData: %v", err)
		}
	}
}

// sanitizeEmail makes a test name safe for the local-part of an email address.
func sanitizeEmail(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			return r
		case r >= 'A' && r <= 'Z':
			return r + ('a' - 'A')
		default:
			return '-'
		}
	}, name)
}

func insertUser(t *testing.T, pool *pgxpool.Pool, email, plan string) (string, string) {
	t.Helper()
	userID := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, username, password_hash, plan) VALUES ($1, $2, $3, 'x', $4)`,
		userID, email, uuid.New().String()[:12], plan,
	)
	if err != nil {
		t.Fatalf("insertUser %s: %v", email, err)
	}
	tok, err := jwtutil.Issue(userID, email, plan, integrationJWTSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("insertUser jwt: %v", err)
	}
	return userID, tok
}

func newTaskServer(t *testing.T, plan string) taskEnv {
	t.Helper()
	pool := testutil.NewTestDB(t)
	cleanTaskTestData(t, pool)
	t.Cleanup(func() { cleanTaskTestData(t, pool) })

	cfg := config.Config{
		JWTSecret:          integrationJWTSecret,
		JWTExpiry:          15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	}

	// Unique email per test so concurrently-cleaned, same-plan tests never
	// collide on the (user_id, email) constraint or wipe each other's rows.
	email := "user-" + sanitizeEmail(t.Name()) + "-" + plan + testEmailDomain
	userID, token := insertUser(t, pool, email, plan)

	taskSvc := task.NewService(task.NewRepository(pool), nil)
	h := handler.Handlers{
		Auth:    auth.NewHandler(auth.NewService(auth.NewRepository(pool), cfg), auth.HandlerConfig{}),
		Area:    area.NewHandler(area.NewService(area.NewRepository(pool))),
		Project: project.NewHandler(project.NewService(project.NewRepository(pool))),
		Task:    task.NewHandler(taskSvc, task.NewSubtaskService(task.NewSubtaskRepository(pool))),
		Bucket:  bucket.NewHandler(bucket.NewService(bucket.NewRepository(pool), taskSvc, nil)),
		AI:      ai.NewHandler(ai.NewService(ai.NewRepository(pool))),
		Billing: billing.NewHandler(billing.NewService(billing.NewRepository(pool))),
	}
	srv := httptest.NewServer(handler.New(cfg, pool, h))
	t.Cleanup(srv.Close)

	areaID := createArea(t, srv, token)
	projectID := createProject(t, srv, token, areaID)

	return taskEnv{srv: srv, token: token, userID: userID, projectID: projectID}
}

// ── http helpers ───────────────────────────────────────────────────────────────

func do(t *testing.T, srv *httptest.Server, method, path string, body any, token string) *http.Response {
	t.Helper()
	var b bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&b).Encode(body); err != nil {
			t.Fatalf("do encode: %v", err)
		}
	}
	req, err := http.NewRequest(method, srv.URL+path, &b)
	if err != nil {
		t.Fatalf("do new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do execute: %v", err)
	}
	return resp
}

func assertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("status = %d, want %d", resp.StatusCode, want)
	}
}

func assertErrCode(t *testing.T, resp *http.Response, want string) {
	t.Helper()
	var env struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decode(t, resp, &env)
	if env.Error == nil || env.Error.Code != want {
		t.Fatalf("error.code = %+v, want %q", env.Error, want)
	}
}

func decode(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

func createArea(t *testing.T, srv *httptest.Server, token string) string {
	t.Helper()
	resp := do(t, srv, http.MethodPost, "/v1/areas", map[string]any{"name": "A", "color": "#3B82F6"}, token)
	assertStatus(t, resp, http.StatusCreated)
	var env struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	decode(t, resp, &env)
	return env.Data.ID
}

func createProject(t *testing.T, srv *httptest.Server, token, areaID string) string {
	t.Helper()
	resp := do(t, srv, http.MethodPost, "/v1/areas/"+areaID+"/projects", map[string]any{"name": "P"}, token)
	assertStatus(t, resp, http.StatusCreated)
	var env struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	decode(t, resp, &env)
	return env.Data.ID
}

func createTask(t *testing.T, env taskEnv, body any) task.TaskView {
	t.Helper()
	resp := do(t, env.srv, http.MethodPost, "/v1/projects/"+env.projectID+"/tasks", body, env.token)
	assertStatus(t, resp, http.StatusCreated)
	var out struct {
		Data task.TaskView `json:"data"`
	}
	decode(t, resp, &out)
	return out.Data
}

// ── tests ──────────────────────────────────────────────────────────────────────

func TestIntegration_Task_QuickAdd_TitleOnly(t *testing.T) {
	env := newTaskServer(t, "free")
	v := createTask(t, env, map[string]any{"title": "Buy milk"})

	if v.Title != "Buy milk" {
		t.Errorf("title = %q", v.Title)
	}
	if v.Status != "inbox" || v.Priority != "medium" || v.Energy != "medium" || !v.RollsOver {
		t.Errorf("defaults wrong: %+v", v)
	}
}

func TestIntegration_Task_CRUD(t *testing.T) {
	env := newTaskServer(t, "pro")
	created := createTask(t, env, map[string]any{"title": "Task", "priority": "high", "energy": "deep"})

	// GET
	resp := do(t, env.srv, http.MethodGet, "/v1/tasks/"+created.ID, nil, env.token)
	assertStatus(t, resp, http.StatusOK)

	// LIST
	resp = do(t, env.srv, http.MethodGet, "/v1/projects/"+env.projectID+"/tasks", nil, env.token)
	assertStatus(t, resp, http.StatusOK)
	var listEnv struct {
		Data struct {
			Items []task.TaskView `json:"items"`
		} `json:"data"`
	}
	decode(t, resp, &listEnv)
	if len(listEnv.Data.Items) != 1 {
		t.Fatalf("list len = %d, want 1", len(listEnv.Data.Items))
	}

	// PATCH -> done sets completedAt
	resp = do(t, env.srv, http.MethodPatch, "/v1/tasks/"+created.ID, map[string]any{"status": "done"}, env.token)
	assertStatus(t, resp, http.StatusOK)
	var patched struct {
		Data task.TaskView `json:"data"`
	}
	decode(t, resp, &patched)
	if patched.Data.Status != "done" || patched.Data.CompletedAt == nil {
		t.Errorf("done patch wrong: %+v", patched.Data)
	}

	// PATCH back -> clears completedAt
	resp = do(t, env.srv, http.MethodPatch, "/v1/tasks/"+created.ID, map[string]any{"status": "active"}, env.token)
	assertStatus(t, resp, http.StatusOK)
	decode(t, resp, &patched)
	if patched.Data.CompletedAt != nil {
		t.Errorf("completedAt should clear, got %v", *patched.Data.CompletedAt)
	}

	// DELETE
	resp = do(t, env.srv, http.MethodDelete, "/v1/tasks/"+created.ID, nil, env.token)
	assertStatus(t, resp, http.StatusNoContent)

	// GET after delete -> 404
	resp = do(t, env.srv, http.MethodGet, "/v1/tasks/"+created.ID, nil, env.token)
	assertStatus(t, resp, http.StatusNotFound)
	assertErrCode(t, resp, "TASK_NOT_FOUND")
}

func TestIntegration_Task_Update_ClearNullableFields(t *testing.T) {
	env := newTaskServer(t, "pro")
	created := createTask(t, env, map[string]any{
		"title":            "Task",
		"notes":            "some notes",
		"scheduledFor":     "2030-01-15",
		"estimatedMinutes": 45,
		"url":              "https://example.com",
	})
	if created.Notes == nil || created.ScheduledFor == nil ||
		created.EstimatedMinutes == nil || created.URL == nil {
		t.Fatalf("setup: expected all fields set, got %+v", created)
	}

	// Explicit null on every nullable field must clear it.
	resp := do(t, env.srv, http.MethodPatch, "/v1/tasks/"+created.ID, map[string]any{
		"notes":            nil,
		"scheduledFor":     nil,
		"estimatedMinutes": nil,
		"url":              nil,
	}, env.token)
	assertStatus(t, resp, http.StatusOK)
	var cleared struct {
		Data task.TaskView `json:"data"`
	}
	decode(t, resp, &cleared)
	if cleared.Data.Notes != nil {
		t.Errorf("notes = %v, want null", *cleared.Data.Notes)
	}
	if cleared.Data.ScheduledFor != nil {
		t.Errorf("scheduledFor = %v, want null", *cleared.Data.ScheduledFor)
	}
	if cleared.Data.EstimatedMinutes != nil {
		t.Errorf("estimatedMinutes = %v, want null", *cleared.Data.EstimatedMinutes)
	}
	if cleared.Data.URL != nil {
		t.Errorf("url = %v, want null", *cleared.Data.URL)
	}
}

func TestIntegration_Task_Update_PartialPatchPreservesFields(t *testing.T) {
	env := newTaskServer(t, "pro")
	created := createTask(t, env, map[string]any{
		"title":            "Task",
		"status":           "active",
		"notes":            "keep me",
		"scheduledFor":     "2030-01-15",
		"estimatedMinutes": 45,
		"url":              "https://example.com",
	})

	// A status-only PATCH (fields absent, not null) must leave the rest untouched.
	resp := do(t, env.srv, http.MethodPatch, "/v1/tasks/"+created.ID,
		map[string]any{"priority": "high"}, env.token)
	assertStatus(t, resp, http.StatusOK)
	var out struct {
		Data task.TaskView `json:"data"`
	}
	decode(t, resp, &out)
	if out.Data.Priority != "high" {
		t.Errorf("priority = %q, want high", out.Data.Priority)
	}
	if out.Data.Notes == nil || *out.Data.Notes != "keep me" {
		t.Errorf("notes = %v, want preserved", out.Data.Notes)
	}
	if out.Data.ScheduledFor == nil {
		t.Error("scheduledFor wiped by partial patch, want preserved")
	}
	if out.Data.EstimatedMinutes == nil || *out.Data.EstimatedMinutes != 45 {
		t.Errorf("estimatedMinutes = %v, want preserved (45)", out.Data.EstimatedMinutes)
	}
	if out.Data.URL == nil || *out.Data.URL != "https://example.com" {
		t.Errorf("url = %v, want preserved", out.Data.URL)
	}
}

func TestIntegration_Task_PlanLimit_ActiveInboxOnly(t *testing.T) {
	env := newTaskServer(t, "free")

	// 50 active tasks -> at the cap.
	for i := 0; i < 50; i++ {
		createTask(t, env, map[string]any{"title": fmt.Sprintf("t%d", i), "status": "active"})
	}

	// 51st active -> 403.
	resp := do(t, env.srv, http.MethodPost, "/v1/projects/"+env.projectID+"/tasks",
		map[string]any{"title": "over", "status": "active"}, env.token)
	assertStatus(t, resp, http.StatusForbidden)
	assertErrCode(t, resp, "PLAN_LIMIT_EXCEEDED")

	// someday does NOT count -> still allowed at the cap.
	resp = do(t, env.srv, http.MethodPost, "/v1/projects/"+env.projectID+"/tasks",
		map[string]any{"title": "later", "status": "someday"}, env.token)
	assertStatus(t, resp, http.StatusCreated)
}

func TestIntegration_Task_CrossUser_Returns404(t *testing.T) {
	env := newTaskServer(t, "free")
	created := createTask(t, env, map[string]any{"title": "mine"})

	// Second user in the same DB.
	pool := testutil.NewTestDB(t)
	_, otherToken := insertUser(t, pool, "intruder"+testEmailDomain, "free")

	for _, m := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		var body any
		if m == http.MethodPatch {
			body = map[string]any{"title": "hax"}
		}
		resp := do(t, env.srv, m, "/v1/tasks/"+created.ID, body, otherToken)
		assertStatus(t, resp, http.StatusNotFound)
		assertErrCode(t, resp, "TASK_NOT_FOUND")
	}
}

func TestIntegration_Task_Create_ProjectNotOwned_Returns404(t *testing.T) {
	env := newTaskServer(t, "free")
	resp := do(t, env.srv, http.MethodPost, "/v1/projects/"+uuid.New().String()+"/tasks",
		map[string]any{"title": "x"}, env.token)
	assertStatus(t, resp, http.StatusNotFound)
	assertErrCode(t, resp, "PROJECT_NOT_FOUND")
}

// ── quick actions ──────────────────────────────────────────────────────────────

func patchTask(t *testing.T, env taskEnv, path string, body any) task.TaskView {
	t.Helper()
	resp := do(t, env.srv, http.MethodPatch, path, body, env.token)
	assertStatus(t, resp, http.StatusOK)
	var out struct {
		Data task.TaskView `json:"data"`
	}
	decode(t, resp, &out)
	return out.Data
}

func TestIntegration_Task_StatusToggle(t *testing.T) {
	env := newTaskServer(t, "pro")
	created := createTask(t, env, map[string]any{"title": "t", "status": "inbox"})

	active := patchTask(t, env, "/v1/tasks/"+created.ID+"/status", map[string]any{"status": "active"})
	if active.Status != "active" || active.CompletedAt != nil {
		t.Fatalf("active wrong: %+v", active)
	}

	done := patchTask(t, env, "/v1/tasks/"+created.ID+"/status", map[string]any{"status": "done"})
	if done.Status != "done" || done.CompletedAt == nil {
		t.Fatalf("done should set completedAt: %+v", done)
	}
}

func TestIntegration_Task_ScheduleAndUnschedule(t *testing.T) {
	env := newTaskServer(t, "pro")
	created := createTask(t, env, map[string]any{"title": "t"})

	scheduled := patchTask(t, env, "/v1/tasks/"+created.ID+"/schedule",
		map[string]any{"scheduledFor": "2026-07-01", "rollsOver": false})
	if scheduled.ScheduledFor == nil || *scheduled.ScheduledFor != "2026-07-01" {
		t.Fatalf("scheduledFor wrong: %+v", scheduled.ScheduledFor)
	}
	if scheduled.RollsOver {
		t.Errorf("rollsOver should be false")
	}

	unscheduled := patchTask(t, env, "/v1/tasks/"+created.ID+"/schedule",
		map[string]any{"scheduledFor": nil})
	if unscheduled.ScheduledFor != nil {
		t.Fatalf("scheduledFor should clear, got %v", *unscheduled.ScheduledFor)
	}

	// bad date -> 400 INVALID_DATE
	resp := do(t, env.srv, http.MethodPatch, "/v1/tasks/"+created.ID+"/schedule",
		map[string]any{"scheduledFor": "not-a-date"}, env.token)
	assertStatus(t, resp, http.StatusBadRequest)
	assertErrCode(t, resp, "INVALID_DATE")
}

func listTasks(t *testing.T, env taskEnv, query string) []task.TaskView {
	t.Helper()
	resp := do(t, env.srv, http.MethodGet, "/v1/projects/"+env.projectID+"/tasks"+query, nil, env.token)
	assertStatus(t, resp, http.StatusOK)
	var out struct {
		Data struct {
			Items []task.TaskView `json:"items"`
		} `json:"data"`
	}
	decode(t, resp, &out)
	return out.Data.Items
}

func TestIntegration_Task_FilterSortSearch(t *testing.T) {
	env := newTaskServer(t, "pro")
	createTask(t, env, map[string]any{"title": "Write spec", "status": "active", "energy": "deep", "priority": "high"})
	createTask(t, env, map[string]any{"title": "Quick reply", "status": "active", "energy": "low", "priority": "low"})
	createTask(t, env, map[string]any{"title": "Read notes", "notes": "review the spec doc", "status": "inbox", "energy": "low"})

	if got := listTasks(t, env, "?energy=low"); len(got) != 2 {
		t.Errorf("energy=low len = %d, want 2", len(got))
	}
	if got := listTasks(t, env, "?status=active&energy=low"); len(got) != 1 || got[0].Title != "Quick reply" {
		t.Errorf("combined filter wrong: %+v", got)
	}
	// search hits title OR notes, case-insensitive: "Write spec" + "review the spec doc"
	if got := listTasks(t, env, "?search=SPEC"); len(got) != 2 {
		t.Errorf("search=spec len = %d, want 2", len(got))
	}
	got := listTasks(t, env, "?sortField=title&sortOrder=asc")
	if len(got) != 3 || got[0].Title != "Quick reply" {
		t.Errorf("title sort wrong: first=%q", got[0].Title)
	}
	resp := do(t, env.srv, http.MethodGet, "/v1/projects/"+env.projectID+"/tasks?sortField=bogus", nil, env.token)
	assertStatus(t, resp, http.StatusBadRequest)
	assertErrCode(t, resp, "INVALID_INPUT")
}

func TestIntegration_Task_ReorderRepack(t *testing.T) {
	env := newTaskServer(t, "pro")
	a := createTask(t, env, map[string]any{"title": "a"})
	b := createTask(t, env, map[string]any{"title": "b"})
	c := createTask(t, env, map[string]any{"title": "c"})
	// initial order: a(0) b(1) c(2)

	// Move c to position 0.
	patchTask(t, env, "/v1/tasks/"+c.ID+"/reorder", map[string]any{"displayOrder": 0})

	// List should now be c, a, b with contiguous 0,1,2.
	resp := do(t, env.srv, http.MethodGet, "/v1/projects/"+env.projectID+"/tasks", nil, env.token)
	assertStatus(t, resp, http.StatusOK)
	var listEnv struct {
		Data struct {
			Items []task.TaskView `json:"items"`
		} `json:"data"`
	}
	decode(t, resp, &listEnv)
	got := listEnv.Data.Items
	if len(got) != 3 {
		t.Fatalf("want 3 tasks, got %d", len(got))
	}
	wantOrder := []string{c.ID, a.ID, b.ID}
	for i, w := range wantOrder {
		if got[i].ID != w {
			t.Errorf("position %d = %s, want %s", i, got[i].ID, w)
		}
		if got[i].DisplayOrder != i {
			t.Errorf("task %d displayOrder = %d, want %d", i, got[i].DisplayOrder, i)
		}
	}
}
