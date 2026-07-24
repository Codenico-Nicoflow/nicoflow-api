//go:build integration

package project_test

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

	"github.com/nicoflow/nicoflow-api/internal/apperror"
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

const integrationJWTSecret = "integration-test-secret-32-bytes!!"

// ── server + helpers ──────────────────────────────────────────────────────────

type testEnv struct {
	srv    *httptest.Server
	token  string
	areaID string
}

// cleanProjectTestData removes only the rows owned by project-integration test users,
// keyed on the email domain "@project-integration.test". This avoids the blanket
// DELETE FROM users that would race against auth integration tests running in parallel.
func cleanProjectTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	// Delete child rows first (FK order), scoped to our test users only.
	queries := []string{
		// subtasks has no user_id — cascade via task_id
		`DELETE FROM subtasks WHERE task_id IN (
			SELECT t.id FROM tasks t
			JOIN users u ON u.id = t.user_id
			WHERE u.email LIKE '%@project-integration.test'
		)`,
		`DELETE FROM tasks      WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@project-integration.test')`,
		`DELETE FROM projects   WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@project-integration.test')`,
		`DELETE FROM areas      WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@project-integration.test')`,
		`DELETE FROM password_reset_tokens WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@project-integration.test')`,
		`DELETE FROM refresh_tokens        WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@project-integration.test')`,
		`DELETE FROM users WHERE email LIKE '%@project-integration.test'`,
	}
	for _, q := range queries {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			t.Fatalf("cleanProjectTestData: %v", err)
		}
	}
}

// insertTestUser inserts a user directly into the DB and returns a signed JWT.
// This avoids calling auth.Register (which stores a refresh token in a separate
// non-transactional step that races with parallel cleanup in other test packages).
func insertTestUser(t *testing.T, pool *pgxpool.Pool, email, plan string) (userID, token string) {
	t.Helper()
	userID = uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO users (id, email, username, password_hash, plan) VALUES ($1, $2, $3, 'x', $4)`,
		userID, email, uuid.New().String()[:12], plan,
	)
	if err != nil {
		t.Fatalf("insertTestUser %s: %v", email, err)
	}
	tok, err := jwtutil.Issue(userID, email, plan, integrationJWTSecret, 15*time.Minute)
	if err != nil {
		t.Fatalf("insertTestUser jwt: %v", err)
	}
	return userID, tok
}

// newProjectServer spins up a real httptest.Server backed by the test DB.
// Returns an env with the server, a JWT for a fresh free-plan user, and a
// pre-created area (most project tests need one).
func newProjectServer(t *testing.T) testEnv {
	t.Helper()
	pool := testutil.NewTestDB(t)
	cleanProjectTestData(t, pool)
	t.Cleanup(func() { cleanProjectTestData(t, pool) })

	cfg := config.Config{
		JWTSecret:          integrationJWTSecret,
		JWTExpiry:          15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	}

	_, token := insertTestUser(t, pool, "usera@project-integration.test", "free")

	taskSvc := task.NewService(task.NewRepository(pool), nil, nil)
	h := handler.Handlers{
		Auth:    auth.NewHandler(auth.NewService(auth.NewRepository(pool), cfg), auth.HandlerConfig{}),
		Area:    area.NewHandler(area.NewService(area.NewRepository(pool), nil)),
		Project: project.NewHandler(project.NewService(project.NewRepository(pool), nil)),
		Task:    task.NewHandler(taskSvc, task.NewSubtaskService(task.NewSubtaskRepository(pool), nil)),
		Bucket:  bucket.NewHandler(bucket.NewService(bucket.NewRepository(pool), taskSvc, nil, nil)),
		AI:      ai.NewHandler(ai.NewService(ai.NewRepository(pool))),
		Billing: billing.NewHandler(billing.NewService(billing.NewRepository(pool))),
	}

	srv := httptest.NewServer(handler.New(cfg, pool, h))
	t.Cleanup(srv.Close)

	// Pre-create an area for convenience.
	aID := doCreateArea(t, srv, token, "Default Area")

	return testEnv{srv: srv, token: token, areaID: aID}
}

func do(t *testing.T, srv *httptest.Server, method, path string, body any, token string) *http.Response {
	t.Helper()
	var b *bytes.Buffer
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("do: marshal: %v", err)
		}
		b = bytes.NewBuffer(raw)
	} else {
		b = &bytes.Buffer{}
	}
	req, err := http.NewRequest(method, srv.URL+path, b)
	if err != nil {
		t.Fatalf("do: new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: execute: %v", err)
	}
	return resp
}

func decodeBody(t *testing.T, resp *http.Response, dst any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decodeBody: %v", err)
	}
}

func assertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("status = %d, want %d", resp.StatusCode, want)
	}
}

func assertHTTPErrCode(t *testing.T, resp *http.Response, wantCode string) {
	t.Helper()
	var env struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeBody(t, resp, &env)
	if env.Error == nil {
		t.Fatalf("expected error envelope, got nil error field")
	}
	if env.Error.Code != wantCode {
		t.Fatalf("error.code = %q, want %q", env.Error.Code, wantCode)
	}
}

func doCreateArea(t *testing.T, srv *httptest.Server, token, name string) string {
	t.Helper()
	resp := do(t, srv, http.MethodPost, "/v1/areas", area.CreateAreaRequest{
		Name: name,
		Icon: "folder",
	}, token)
	assertStatus(t, resp, http.StatusCreated)
	var env struct {
		Data area.AreaView `json:"data"`
	}
	decodeBody(t, resp, &env)
	return env.Data.ID
}

func createProject(t *testing.T, srv *httptest.Server, token, areaID, name string) project.ProjectView {
	t.Helper()
	resp := do(t, srv, http.MethodPost, fmt.Sprintf("/v1/areas/%s/projects", areaID),
		project.CreateProjectRequest{Name: name, Status: "active", FolderIcon: "folder"}, token)
	assertStatus(t, resp, http.StatusCreated)
	var env struct {
		Data project.ProjectView `json:"data"`
	}
	decodeBody(t, resp, &env)
	return env.Data
}

// ── Tests: Create ─────────────────────────────────────────────────────────────

// TestIntegration_Project_Create_HappyPath covers test matrix row #16.
func TestIntegration_Project_Create_HappyPath(t *testing.T) {
	env := newProjectServer(t)

	resp := do(t, env.srv, http.MethodPost, fmt.Sprintf("/v1/areas/%s/projects", env.areaID),
		project.CreateProjectRequest{
			Name:       "Q3 Launch",
			Status:     "active",
			FolderIcon: "folder",
		}, env.token)
	assertStatus(t, resp, http.StatusCreated)

	var pEnv struct {
		Data  project.ProjectView `json:"data"`
		Error any                 `json:"error"`
	}
	decodeBody(t, resp, &pEnv)

	if pEnv.Data.ID == "" {
		t.Error("expected non-empty id (string)")
	}
	if pEnv.Data.Name != "Q3 Launch" {
		t.Errorf("name = %q, want %q", pEnv.Data.Name, "Q3 Launch")
	}
	if pEnv.Data.AreaID != env.areaID {
		t.Errorf("areaId = %q, want %q", pEnv.Data.AreaID, env.areaID)
	}
	if pEnv.Data.DueDate != nil {
		t.Errorf("dueDate = %v, want null", pEnv.Data.DueDate)
	}
	if pEnv.Error != nil {
		t.Errorf("expected error: null, got %v", pEnv.Error)
	}
}

// TestIntegration_Project_Create_ThemedFolderIcon guards the DB path for the
// unified icon allowlist: a themed icon valid in project.AllowedIcons must
// persist (not be rejected by a stale folder_icon CHECK -> 500). Regression for
// the migration-013 CHECK dropped in migration 022. An unknown icon still 422s.
func TestIntegration_Project_Create_ThemedFolderIcon(t *testing.T) {
	env := newProjectServer(t)

	for _, icon := range []string{"rocket", "briefcase", "heart-pulse"} {
		resp := do(t, env.srv, http.MethodPost, fmt.Sprintf("/v1/areas/%s/projects", env.areaID),
			project.CreateProjectRequest{Name: "P-" + icon, Status: "active", FolderIcon: icon}, env.token)
		assertStatus(t, resp, http.StatusCreated)
	}

	resp := do(t, env.srv, http.MethodPost, fmt.Sprintf("/v1/areas/%s/projects", env.areaID),
		project.CreateProjectRequest{Name: "bad", Status: "active", FolderIcon: "not-a-real-icon"}, env.token)
	assertStatus(t, resp, http.StatusUnprocessableEntity)
}

// TestIntegration_Project_Create_PlanLimit covers test matrix row #17.
func TestIntegration_Project_Create_PlanLimit(t *testing.T) {
	env := newProjectServer(t)

	for i := range 5 {
		createProject(t, env.srv, env.token, env.areaID, fmt.Sprintf("Project %d", i+1))
	}

	resp := do(t, env.srv, http.MethodPost, fmt.Sprintf("/v1/areas/%s/projects", env.areaID),
		project.CreateProjectRequest{Name: "Project 6", Status: "active", FolderIcon: "folder"}, env.token)
	assertStatus(t, resp, http.StatusForbidden)
	assertHTTPErrCode(t, resp, apperror.ErrPlanLimitExceeded)
}

// TestIntegration_Project_Create_PlanLimitIsGlobal covers test matrix row #18.
// The 5-project limit applies across all areas, not per area.
func TestIntegration_Project_Create_PlanLimitIsGlobal(t *testing.T) {
	env := newProjectServer(t)

	// Create a second area.
	areaB := doCreateArea(t, env.srv, env.token, "Area B")

	// Distribute: 3 in area A, 2 in area B = 5 total.
	for i := range 3 {
		createProject(t, env.srv, env.token, env.areaID, fmt.Sprintf("A-Project %d", i+1))
	}
	for i := range 2 {
		createProject(t, env.srv, env.token, areaB, fmt.Sprintf("B-Project %d", i+1))
	}

	// 6th project in area B must still be rejected.
	resp := do(t, env.srv, http.MethodPost, fmt.Sprintf("/v1/areas/%s/projects", areaB),
		project.CreateProjectRequest{Name: "Over limit", Status: "active", FolderIcon: "folder"}, env.token)
	assertStatus(t, resp, http.StatusForbidden)
	assertHTTPErrCode(t, resp, apperror.ErrPlanLimitExceeded)
}

// TestIntegration_Project_Create_CrossUserAreaID covers test matrix row #19.
// Posting to an area that doesn't exist (or belongs to another user) returns 404 AREA_NOT_FOUND.
func TestIntegration_Project_Create_CrossUserAreaID(t *testing.T) {
	env := newProjectServer(t)

	resp := do(t, env.srv, http.MethodPost, "/v1/areas/nonexistent-area-id/projects",
		project.CreateProjectRequest{Name: "X", Status: "active", FolderIcon: "folder"}, env.token)
	assertStatus(t, resp, http.StatusNotFound)
	assertHTTPErrCode(t, resp, apperror.ErrAreaNotFound)
}

// TestIntegration_Project_Create_InvalidStatus covers test matrix row #20.
func TestIntegration_Project_Create_InvalidStatus(t *testing.T) {
	env := newProjectServer(t)

	resp := do(t, env.srv, http.MethodPost, fmt.Sprintf("/v1/areas/%s/projects", env.areaID),
		map[string]string{"name": "X", "status": "unknown", "folderIcon": "folder"}, env.token)
	assertStatus(t, resp, http.StatusUnprocessableEntity)
	assertHTTPErrCode(t, resp, apperror.ErrInvalidStatus)
}

// ── Tests: List ───────────────────────────────────────────────────────────────

// TestIntegration_Project_List_AllOrdered covers test matrix row #21.
func TestIntegration_Project_List_AllOrdered(t *testing.T) {
	env := newProjectServer(t)
	areaB := doCreateArea(t, env.srv, env.token, "Area B")

	createProject(t, env.srv, env.token, env.areaID, "P-A1")
	createProject(t, env.srv, env.token, areaB, "P-B1")

	resp := do(t, env.srv, http.MethodGet, "/v1/projects", nil, env.token)
	assertStatus(t, resp, http.StatusOK)

	var listEnv struct {
		Data struct {
			Items      []project.ProjectView `json:"items"`
			NextCursor string                `json:"nextCursor"`
		} `json:"data"`
	}
	decodeBody(t, resp, &listEnv)

	if len(listEnv.Data.Items) != 2 {
		t.Errorf("expected 2 projects, got %d", len(listEnv.Data.Items))
	}
}

// TestIntegration_Project_List_FilterByArea covers test matrix row #22.
func TestIntegration_Project_List_FilterByArea(t *testing.T) {
	env := newProjectServer(t)
	areaB := doCreateArea(t, env.srv, env.token, "Area B")

	pA := createProject(t, env.srv, env.token, env.areaID, "Area A Project")
	createProject(t, env.srv, env.token, areaB, "Area B Project")

	resp := do(t, env.srv, http.MethodGet, fmt.Sprintf("/v1/areas/%s/projects", env.areaID), nil, env.token)
	assertStatus(t, resp, http.StatusOK)

	var listEnv struct {
		Data struct {
			Items []project.ProjectView `json:"items"`
		} `json:"data"`
	}
	decodeBody(t, resp, &listEnv)

	if len(listEnv.Data.Items) != 1 {
		t.Fatalf("expected 1 project for area A, got %d", len(listEnv.Data.Items))
	}
	if listEnv.Data.Items[0].ID != pA.ID {
		t.Errorf("item ID = %q, want %q", listEnv.Data.Items[0].ID, pA.ID)
	}
}

// ── Tests: Update ─────────────────────────────────────────────────────────────

// TestIntegration_Project_Update_MoveToAnotherArea covers test matrix row #23.
func TestIntegration_Project_Update_MoveToAnotherArea(t *testing.T) {
	env := newProjectServer(t)
	areaB := doCreateArea(t, env.srv, env.token, "Area B")
	p := createProject(t, env.srv, env.token, env.areaID, "Moveable")

	resp := do(t, env.srv, http.MethodPatch, "/v1/projects/"+p.ID,
		map[string]any{"areaId": areaB}, env.token)
	assertStatus(t, resp, http.StatusOK)

	var pEnv struct {
		Data project.ProjectView `json:"data"`
	}
	decodeBody(t, resp, &pEnv)
	if pEnv.Data.AreaID != areaB {
		t.Errorf("areaId = %q, want %q", pEnv.Data.AreaID, areaB)
	}

	// Confirm it appears in area B's list.
	listResp := do(t, env.srv, http.MethodGet, fmt.Sprintf("/v1/areas/%s/projects", areaB), nil, env.token)
	assertStatus(t, listResp, http.StatusOK)
	var listEnv struct {
		Data struct {
			Items []project.ProjectView `json:"items"`
		} `json:"data"`
	}
	decodeBody(t, listResp, &listEnv)
	found := false
	for _, item := range listEnv.Data.Items {
		if item.ID == p.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("moved project not found in area B's list")
	}
}

// TestIntegration_Project_Update_DetachRejected covers test matrix row #24:
// a project must always belong to an area, so an empty areaId is rejected.
func TestIntegration_Project_Update_DetachRejected(t *testing.T) {
	env := newProjectServer(t)
	p := createProject(t, env.srv, env.token, env.areaID, "Detachable")

	resp := do(t, env.srv, http.MethodPatch, "/v1/projects/"+p.ID,
		map[string]any{"areaId": ""}, env.token)
	assertStatus(t, resp, http.StatusUnprocessableEntity)
	assertHTTPErrCode(t, resp, apperror.ErrInvalidInput)
}

// TestIntegration_Project_Update_MoveToUnknownArea: moving into a foreign or
// missing area is rejected by the user-scoped area subquery.
func TestIntegration_Project_Update_MoveToUnknownArea(t *testing.T) {
	env := newProjectServer(t)
	p := createProject(t, env.srv, env.token, env.areaID, "Moveable")

	resp := do(t, env.srv, http.MethodPatch, "/v1/projects/"+p.ID,
		map[string]any{"areaId": "nonexistent-area-id"}, env.token)
	assertStatus(t, resp, http.StatusNotFound)
	assertHTTPErrCode(t, resp, apperror.ErrAreaNotFound)
}

// ── Tests: Delete ─────────────────────────────────────────────────────────────

// TestIntegration_Project_Delete_NullsTaskProjectID covers test matrix row #25.
// The FK is ON DELETE SET NULL — deleting a project sets tasks.project_id to NULL
// (tasks survive, un-assigned). Task HTTP endpoints are stubs; verified via SQL.
func TestIntegration_Project_Delete_NullsTaskProjectID(t *testing.T) {
	pool := testutil.NewTestDB(t)
	env := newProjectServer(t)
	p := createProject(t, env.srv, env.token, env.areaID, "With Tasks")

	// Insert a task directly into the DB (task HTTP create is a stub).
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tasks (id, user_id, project_id, title, status)
		 VALUES (gen_random_uuid()::text,
		         (SELECT id FROM users WHERE email = $1),
		         $2, 'My Task', 'inbox')`,
		"usera@project-integration.test", p.ID,
	)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}

	// Delete the project.
	delResp := do(t, env.srv, http.MethodDelete, "/v1/projects/"+p.ID, nil, env.token)
	assertStatus(t, delResp, http.StatusNoContent)

	// project_id must be NULLed by ON DELETE SET NULL — task still exists.
	var projectIDIsNull bool
	if err := pool.QueryRow(context.Background(),
		`SELECT project_id IS NULL FROM tasks WHERE user_id = (SELECT id FROM users WHERE email = $1)`,
		"usera@project-integration.test",
	).Scan(&projectIDIsNull); err != nil {
		t.Fatalf("query task: %v", err)
	}
	if !projectIDIsNull {
		t.Error("expected task.project_id = NULL after project delete (ON DELETE SET NULL)")
	}
}

// ── Tests: Cross-user isolation ───────────────────────────────────────────────

// TestIntegration_Project_CrossUser_Returns404 covers test matrix row #26.
func TestIntegration_Project_CrossUser_Returns404(t *testing.T) {
	envA := newProjectServer(t)
	pA := createProject(t, envA.srv, envA.token, envA.areaID, "User A Project")

	// Create user B via direct DB insert (avoids auth.Register refresh-token race).
	pool := testutil.NewTestDB(t)
	_, tokenB := insertTestUser(t, pool, "userb@project-integration.test", "free")

	verbs := []struct {
		method string
		body   any
	}{
		{http.MethodGet, nil},
		{http.MethodPatch, map[string]string{"name": "Hijack"}},
		{http.MethodDelete, nil},
	}
	for _, v := range verbs {
		t.Run(v.method, func(t *testing.T) {
			resp := do(t, envA.srv, v.method, "/v1/projects/"+pA.ID, v.body, tokenB)
			assertStatus(t, resp, http.StatusNotFound)
			assertHTTPErrCode(t, resp, apperror.ErrProjectNotFound)
		})
	}
}

// ── Tests: Auth ───────────────────────────────────────────────────────────────

// TestIntegration_Project_NoJWT covers test matrix row #27.
func TestIntegration_Project_NoJWT(t *testing.T) {
	env := newProjectServer(t)

	endpoints := []struct {
		method string
		path   string
		body   any
	}{
		{http.MethodGet, "/v1/projects", nil},
		{http.MethodPost, fmt.Sprintf("/v1/areas/%s/projects", env.areaID),
			project.CreateProjectRequest{Name: "X", Status: "active", FolderIcon: "folder"}},
		{http.MethodGet, "/v1/projects/nonexistent-id", nil},
		{http.MethodPatch, "/v1/projects/nonexistent-id", map[string]string{"name": "X"}},
		{http.MethodDelete, "/v1/projects/nonexistent-id", nil},
	}

	for _, e := range endpoints {
		t.Run(e.method+" "+e.path, func(t *testing.T) {
			resp := do(t, env.srv, e.method, e.path, e.body, "")
			assertStatus(t, resp, http.StatusUnauthorized)
			assertHTTPErrCode(t, resp, apperror.ErrUnauthorized)
		})
	}
}

// ── Tests: Reorder ────────────────────────────────────────────────────────────

// TestIntegration_Project_Reorder_FullSet covers test matrix row #28.
func TestIntegration_Project_Reorder_FullSet(t *testing.T) {
	env := newProjectServer(t)
	p1 := createProject(t, env.srv, env.token, env.areaID, "P1")
	p2 := createProject(t, env.srv, env.token, env.areaID, "P2")
	p3 := createProject(t, env.srv, env.token, env.areaID, "P3")

	resp := do(t, env.srv, http.MethodPatch, "/v1/projects/reorder", project.ReorderRequest{
		Items: []project.ReorderItem{
			{ID: p3.ID, DisplayOrder: 0},
			{ID: p1.ID, DisplayOrder: 1},
			{ID: p2.ID, DisplayOrder: 2},
		},
	}, env.token)
	assertStatus(t, resp, http.StatusOK)

	var env2 struct {
		Data struct {
			Updated int `json:"updated"`
		} `json:"data"`
	}
	decodeBody(t, resp, &env2)
	if env2.Data.Updated != 3 {
		t.Errorf("updated = %d, want 3", env2.Data.Updated)
	}

	// Verify new order persisted.
	listResp := do(t, env.srv, http.MethodGet, "/v1/projects", nil, env.token)
	assertStatus(t, listResp, http.StatusOK)
	var listEnv struct {
		Data struct {
			Items []project.ProjectView `json:"items"`
		} `json:"data"`
	}
	decodeBody(t, listResp, &listEnv)
	if listEnv.Data.Items[0].ID != p3.ID {
		t.Errorf("items[0] = %q, want p3 (%q)", listEnv.Data.Items[0].ID, p3.ID)
	}
}

// TestIntegration_Project_Reorder_PartialSet covers test matrix row #29.
func TestIntegration_Project_Reorder_PartialSet(t *testing.T) {
	env := newProjectServer(t)
	p1 := createProject(t, env.srv, env.token, env.areaID, "P1")
	p2 := createProject(t, env.srv, env.token, env.areaID, "P2")
	createProject(t, env.srv, env.token, env.areaID, "P3") // unchanged

	resp := do(t, env.srv, http.MethodPatch, "/v1/projects/reorder", project.ReorderRequest{
		Items: []project.ReorderItem{
			{ID: p2.ID, DisplayOrder: 0},
			{ID: p1.ID, DisplayOrder: 1},
		},
	}, env.token)
	assertStatus(t, resp, http.StatusOK)

	var env2 struct {
		Data struct {
			Updated int `json:"updated"`
		} `json:"data"`
	}
	decodeBody(t, resp, &env2)
	if env2.Data.Updated != 2 {
		t.Errorf("updated = %d, want 2", env2.Data.Updated)
	}
}

// TestIntegration_Project_Reorder_CrossUserRejected covers test matrix rows #30.
func TestIntegration_Project_Reorder_CrossUserRejected(t *testing.T) {
	envA := newProjectServer(t)
	pA := createProject(t, envA.srv, envA.token, envA.areaID, "User A Project")

	pool := testutil.NewTestDB(t)
	_, tokenB := insertTestUser(t, pool, "userb-reorder@project-integration.test", "free")
	areaBID := doCreateArea(t, envA.srv, tokenB, "User B Area")
	pB := createProject(t, envA.srv, tokenB, areaBID, "User B Project")

	resp := do(t, envA.srv, http.MethodPatch, "/v1/projects/reorder", project.ReorderRequest{
		Items: []project.ReorderItem{
			{ID: pB.ID, DisplayOrder: 10},
			{ID: pA.ID, DisplayOrder: 20}, // belongs to user A
		},
	}, tokenB)
	assertStatus(t, resp, http.StatusNotFound)
	assertHTTPErrCode(t, resp, apperror.ErrProjectNotFound)

	// User B's own project must be unchanged (atomic rollback).
	getResp := do(t, envA.srv, http.MethodGet, "/v1/projects/"+pB.ID, nil, tokenB)
	assertStatus(t, getResp, http.StatusOK)
	var getEnv struct {
		Data project.ProjectView `json:"data"`
	}
	decodeBody(t, getResp, &getEnv)
	if getEnv.Data.DisplayOrder == 10 {
		t.Error("expected atomic rollback: user B's project displayOrder must not have changed")
	}
}

func TestIntegration_Project_Update_ClearNullableFields(t *testing.T) {
	env := newProjectServer(t)
	resp := do(t, env.srv, http.MethodPost, fmt.Sprintf("/v1/areas/%s/projects", env.areaID),
		map[string]any{
			"name":        "P",
			"status":      "active",
			"folderIcon":  "folder",
			"dueDate":     "2030-01-15T00:00:00Z",
			"description": "some description",
		}, env.token)
	assertStatus(t, resp, http.StatusCreated)
	var created struct {
		Data project.ProjectView `json:"data"`
	}
	decodeBody(t, resp, &created)
	if created.Data.DueDate == nil || created.Data.Description == nil {
		t.Fatalf("setup: expected dueDate + description set, got %+v", created.Data)
	}

	// Explicit null must clear both.
	resp = do(t, env.srv, http.MethodPatch, "/v1/projects/"+created.Data.ID,
		map[string]any{"dueDate": nil, "description": nil}, env.token)
	assertStatus(t, resp, http.StatusOK)
	var cleared struct {
		Data project.ProjectView `json:"data"`
	}
	decodeBody(t, resp, &cleared)
	if cleared.Data.DueDate != nil {
		t.Errorf("dueDate = %v, want null", *cleared.Data.DueDate)
	}
	if cleared.Data.Description != nil {
		t.Errorf("description = %v, want null", *cleared.Data.Description)
	}
}

func TestIntegration_Project_Update_PartialPatchPreservesFields(t *testing.T) {
	env := newProjectServer(t)
	resp := do(t, env.srv, http.MethodPost, fmt.Sprintf("/v1/areas/%s/projects", env.areaID),
		map[string]any{
			"name":        "P",
			"status":      "active",
			"folderIcon":  "folder",
			"dueDate":     "2030-01-15T00:00:00Z",
			"description": "keep me",
		}, env.token)
	assertStatus(t, resp, http.StatusCreated)
	var created struct {
		Data project.ProjectView `json:"data"`
	}
	decodeBody(t, resp, &created)

	// A name-only PATCH (dueDate + description absent) must preserve them.
	resp = do(t, env.srv, http.MethodPatch, "/v1/projects/"+created.Data.ID,
		map[string]any{"name": "Renamed"}, env.token)
	assertStatus(t, resp, http.StatusOK)
	var out struct {
		Data project.ProjectView `json:"data"`
	}
	decodeBody(t, resp, &out)
	if out.Data.Name != "Renamed" {
		t.Errorf("name = %q, want Renamed", out.Data.Name)
	}
	if out.Data.DueDate == nil {
		t.Error("dueDate wiped by partial patch, want preserved")
	}
	if out.Data.Description == nil || *out.Data.Description != "keep me" {
		t.Errorf("description = %v, want preserved", out.Data.Description)
	}
}
