//go:build integration

package task_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

	userID, token := insertUser(t, pool, "user"+plan+testEmailDomain, plan)

	h := handler.Handlers{
		Auth:    auth.NewHandler(auth.NewService(auth.NewRepository(pool), cfg), auth.HandlerConfig{}),
		Area:    area.NewHandler(area.NewService(area.NewRepository(pool))),
		Project: project.NewHandler(project.NewService(project.NewRepository(pool))),
		Task:    task.NewHandler(task.NewService(task.NewRepository(pool)), task.NewSubtaskService(task.NewSubtaskRepository(pool))),
		Bucket:  bucket.NewHandler(bucket.NewService(bucket.NewRepository(pool))),
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
