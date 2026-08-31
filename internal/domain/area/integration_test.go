//go:build integration

package area_test

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
	"github.com/nicoflow/nicoflow-api/internal/domain/area"
	"github.com/nicoflow/nicoflow-api/internal/domain/auth"
	"github.com/nicoflow/nicoflow-api/internal/domain/project"
	"github.com/nicoflow/nicoflow-api/internal/handler"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
	"github.com/nicoflow/nicoflow-api/pkg/jwtutil"
)

const (
	testJWTSecret = "integration-test-secret-32-bytes!!"
	testJWTExpiry = 15 * time.Minute
)

// cleanAreaTestData deletes only rows belonging to area-integration test users
// (email domain "@integration.test"), avoiding cross-package races with auth tests.
func cleanAreaTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	queries := []string{
		`DELETE FROM projects WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@integration.test')`,
		`DELETE FROM areas    WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@integration.test')`,
		`DELETE FROM refresh_tokens        WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@integration.test')`,
		`DELETE FROM password_reset_tokens WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%@integration.test')`,
		`DELETE FROM users WHERE email LIKE '%@integration.test'`,
	}
	for _, q := range queries {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			t.Fatalf("cleanAreaTestData: %v", err)
		}
	}
}

// ── server helpers ─────────────────────────────────────────────────────────────

func newAreaServer(t *testing.T) (*httptest.Server, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewTestDB(t)
	cleanAreaTestData(t, pool)
	t.Cleanup(func() { cleanAreaTestData(t, pool) })

	cfg := config.Config{
		JWTSecret:          testJWTSecret,
		JWTExpiry:          testJWTExpiry,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	}

	authRepo := auth.NewRepository(pool)
	authSvc := auth.NewService(authRepo, cfg)

	areaSvc := area.NewService(area.NewRepository(pool), nil)
	projSvc := project.NewService(project.NewRepository(pool), nil, nil)

	// Remaining handlers are stubs — pass nils through the handler package's stub paths.
	h := handler.Handlers{
		Auth:    auth.NewHandler(authSvc, auth.HandlerConfig{}),
		Area:    area.NewHandler(areaSvc),
		Project: project.NewHandler(projSvc),
	}

	srv := httptest.NewServer(handler.New(cfg, pool, h))
	t.Cleanup(srv.Close)
	return srv, pool
}

// mustCreateUser inserts a user directly and returns a signed JWT for them.
func mustCreateUser(t *testing.T, pool *pgxpool.Pool, plan string) (userID, token string) {
	t.Helper()
	userID = uuid.New().String()
	email := fmt.Sprintf("%s@integration.test", userID[:8])
	_, err := pool.Exec(context.Background(), `
		INSERT INTO users (id, email, username, password_hash, plan)
		VALUES ($1, $2, $3, 'x', $4)`,
		userID, email, userID[:8], plan,
	)
	if err != nil {
		t.Fatalf("mustCreateUser: %v", err)
	}
	tok, err := jwtutil.Issue(userID, email, plan, testJWTSecret, testJWTExpiry)
	if err != nil {
		t.Fatalf("mustCreateUser: issue jwt: %v", err)
	}
	return userID, tok
}

// mustCreateArea inserts an area via the API and returns its ID.
func mustCreateArea(t *testing.T, srv *httptest.Server, token, name string) string {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": name, "color": "#3B82F6", "icon": "folder"})
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/areas", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("mustCreateArea: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("mustCreateArea: want 201, got %d", resp.StatusCode)
	}
	var env struct {
		Data area.AreaView `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("mustCreateArea: decode: %v", err)
	}
	return env.Data.ID
}

// do sends an authenticated JSON request and returns the response.
func do(t *testing.T, method, url, token string, body any) *http.Response {
	t.Helper()
	var b *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		b = bytes.NewReader(raw)
	} else {
		b = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, url, b)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, url, err)
	}
	return resp
}

// assertStatus fatals if the response status does not match want.
func assertStatus(t *testing.T, resp *http.Response, want int) {
	t.Helper()
	if resp.StatusCode != want {
		t.Fatalf("want status %d, got %d", want, resp.StatusCode)
	}
}

// assertErrorCode decodes the envelope and checks error.code.
func assertErrorCode(t *testing.T, resp *http.Response, wantCode string) {
	t.Helper()
	var env struct {
		Error *struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("assertErrorCode: decode: %v", err)
	}
	if env.Error == nil {
		t.Fatalf("assertErrorCode: expected error object, got nil")
	}
	if env.Error.Code != wantCode {
		t.Fatalf("assertErrorCode: want %q, got %q", wantCode, env.Error.Code)
	}
}

// ── Area CRUD ─────────────────────────────────────────────────────────────────

func TestIntegration_Area_Create_HappyPath(t *testing.T) {
	srv, pool := newAreaServer(t)
	_, token := mustCreateUser(t, pool, "free")

	body := map[string]string{"name": "Health", "color": "#10B981", "icon": "folder"}
	resp := do(t, http.MethodPost, srv.URL+"/v1/areas", token, body)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusCreated)

	var env struct {
		Data  area.AreaView `json:"data"`
		Error any           `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Error != nil {
		t.Fatalf("expected error: null, got %v", env.Error)
	}
	if env.Data.ID == "" {
		t.Error("expected non-empty id (string)")
	}
	if env.Data.Name != "Health" {
		t.Errorf("name: want %q, got %q", "Health", env.Data.Name)
	}
	if env.Data.Color != "#10B981" {
		t.Errorf("color: want %q, got %q", "#10B981", env.Data.Color)
	}

	// Verify it appears in GET /v1/areas.
	listResp := do(t, http.MethodGet, srv.URL+"/v1/areas", token, nil)
	defer listResp.Body.Close()
	assertStatus(t, listResp, http.StatusOK)
	var listEnv struct {
		Data struct {
			Items []area.AreaView `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listEnv); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	found := false
	for _, a := range listEnv.Data.Items {
		if a.ID == env.Data.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("created area not found in GET /v1/areas")
	}
}

func TestIntegration_Area_Create_PlanLimitAtFourth(t *testing.T) {
	srv, pool := newAreaServer(t)
	_, token := mustCreateUser(t, pool, "free")

	for i := 0; i < 3; i++ {
		mustCreateArea(t, srv, token, fmt.Sprintf("Area %d", i+1))
	}

	resp := do(t, http.MethodPost, srv.URL+"/v1/areas", token, map[string]string{"name": "Area 4"})
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusForbidden)
	assertErrorCode(t, resp, apperror.ErrPlanLimitExceeded)
}

func TestIntegration_Area_Create_DuplicateName(t *testing.T) {
	srv, pool := newAreaServer(t)
	_, token := mustCreateUser(t, pool, "free")
	mustCreateArea(t, srv, token, "Work")

	resp := do(t, http.MethodPost, srv.URL+"/v1/areas", token, map[string]string{"name": "Work"})
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusConflict)
	assertErrorCode(t, resp, apperror.ErrDuplicateName)
}

func TestIntegration_Area_Create_EmptyName(t *testing.T) {
	srv, pool := newAreaServer(t)
	_, token := mustCreateUser(t, pool, "free")

	resp := do(t, http.MethodPost, srv.URL+"/v1/areas", token, map[string]string{"name": ""})
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusUnprocessableEntity)
	assertErrorCode(t, resp, apperror.ErrInvalidInput)
}

func TestIntegration_Area_List_SortedByDisplayOrder(t *testing.T) {
	srv, pool := newAreaServer(t)
	_, token := mustCreateUser(t, pool, "pro")

	// Create 3 areas — they'll get displayOrder 0 by default.
	for _, name := range []string{"C", "A", "B"} {
		mustCreateArea(t, srv, token, name)
	}

	resp := do(t, http.MethodGet, srv.URL+"/v1/areas", token, nil)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	var env struct {
		Data struct {
			Items      []area.AreaView `json:"items"`
			NextCursor string          `json:"nextCursor"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(env.Data.Items) != 3 {
		t.Fatalf("want 3 items, got %d", len(env.Data.Items))
	}
	for i := 1; i < len(env.Data.Items); i++ {
		if env.Data.Items[i].DisplayOrder < env.Data.Items[i-1].DisplayOrder {
			t.Errorf("items not sorted by displayOrder ASC at index %d", i)
		}
	}
}

func TestIntegration_Area_ListWithProjects(t *testing.T) {
	srv, pool := newAreaServer(t)
	userID, token := mustCreateUser(t, pool, "pro")
	areaID := mustCreateArea(t, srv, token, "Work")

	// Insert a project directly.
	_, err := pool.Exec(context.Background(), `
		INSERT INTO projects (id, user_id, area_id, name, status, folder_icon)
		VALUES ($1, $2, $3, 'P1', 'active', 'folder')`,
		uuid.New().String(), userID, areaID,
	)
	if err != nil {
		t.Fatalf("insert project: %v", err)
	}

	resp := do(t, http.MethodGet, srv.URL+"/v1/areas/with-projects", token, nil)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	var env struct {
		Data []struct {
			ID       string                `json:"id"`
			Projects []project.ProjectView `json:"projects"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	found := false
	for _, a := range env.Data {
		if a.ID == areaID {
			if len(a.Projects) != 1 {
				t.Errorf("expected 1 project nested in area, got %d", len(a.Projects))
			}
			found = true
		}
	}
	if !found {
		t.Error("created area not found in with-projects response")
	}
}

func TestIntegration_Area_Patch_UpdatesFields(t *testing.T) {
	srv, pool := newAreaServer(t)
	_, token := mustCreateUser(t, pool, "free")
	id := mustCreateArea(t, srv, token, "Old Name")

	resp := do(t, http.MethodPatch, srv.URL+"/v1/areas/"+id, token, map[string]string{
		"name":  "New Name",
		"color": "#10B981",
	})
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	var env struct {
		Data area.AreaView `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.Name != "New Name" {
		t.Errorf("name: want %q, got %q", "New Name", env.Data.Name)
	}

	// Confirm GET reflects the update.
	getResp := do(t, http.MethodGet, srv.URL+"/v1/areas/"+id, token, nil)
	defer getResp.Body.Close()
	assertStatus(t, getResp, http.StatusOK)
	var getEnv struct {
		Data area.AreaView `json:"data"`
	}
	if err := json.NewDecoder(getResp.Body).Decode(&getEnv); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if getEnv.Data.Name != "New Name" {
		t.Errorf("GET name: want %q, got %q", "New Name", getEnv.Data.Name)
	}
}

func TestIntegration_Area_Patch_CrossUser_Returns404(t *testing.T) {
	srv, pool := newAreaServer(t)
	_, tokenA := mustCreateUser(t, pool, "free")
	_, tokenB := mustCreateUser(t, pool, "free")

	idA := mustCreateArea(t, srv, tokenA, "User A Area")

	resp := do(t, http.MethodPatch, srv.URL+"/v1/areas/"+idA, tokenB, map[string]string{"name": "Hack"})
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusNotFound)
	assertErrorCode(t, resp, apperror.ErrAreaNotFound)
}

func TestIntegration_Area_Delete_CascadesProjectsAndTasks(t *testing.T) {
	srv, pool := newAreaServer(t)
	userID, token := mustCreateUser(t, pool, "pro")
	areaID := mustCreateArea(t, srv, token, "ToDelete")

	// Insert two projects into the area, each with a task, directly.
	projIDs := []string{uuid.New().String(), uuid.New().String()}
	taskIDs := []string{uuid.New().String(), uuid.New().String()}
	for i, pid := range projIDs {
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO projects (id, user_id, area_id, name, status, folder_icon)
			VALUES ($1, $2, $3, $4, 'active', 'folder')`,
			pid, userID, areaID, fmt.Sprintf("Project %d", i+1),
		); err != nil {
			t.Fatalf("insert project: %v", err)
		}
		if _, err := pool.Exec(context.Background(), `
			INSERT INTO tasks (id, user_id, project_id, title, status)
			VALUES ($1, $2, $3, $4, 'active')`,
			taskIDs[i], userID, pid, fmt.Sprintf("Task %d", i+1),
		); err != nil {
			t.Fatalf("insert task: %v", err)
		}
	}

	resp := do(t, http.MethodDelete, srv.URL+"/v1/areas/"+areaID, token, nil)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusNoContent)

	// Deleting the area must cascade-delete its projects...
	for _, pid := range projIDs {
		var count int
		if err := pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM projects WHERE id = $1`, pid).Scan(&count); err != nil {
			t.Fatalf("count project %s: %v", pid, err)
		}
		if count != 0 {
			t.Errorf("project %s should be deleted with its area, still present", pid)
		}
	}
	// ...and cascade-delete their tasks in turn (tasks.project_id is
	// ON DELETE CASCADE), matching the single-project delete behaviour.
	for _, tid := range taskIDs {
		var count int
		if err := pool.QueryRow(context.Background(),
			`SELECT COUNT(*) FROM tasks WHERE id = $1`, tid).Scan(&count); err != nil {
			t.Fatalf("count task %s: %v", tid, err)
		}
		if count != 0 {
			t.Errorf("task %s should be deleted with its project/area, still present", tid)
		}
	}
}

func TestIntegration_Area_NoJWT_Returns401(t *testing.T) {
	srv, pool := newAreaServer(t)
	_, token := mustCreateUser(t, pool, "free")
	id := mustCreateArea(t, srv, token, "Private")

	urls := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/v1/areas"},
		{http.MethodGet, "/v1/areas/with-projects"},
		{http.MethodPost, "/v1/areas"},
		{http.MethodGet, "/v1/areas/" + id},
		{http.MethodPatch, "/v1/areas/" + id},
		{http.MethodDelete, "/v1/areas/" + id},
		{http.MethodPatch, "/v1/areas/reorder"},
	}
	for _, tc := range urls {
		resp := do(t, tc.method, srv.URL+tc.path, "" /* no token */, map[string]string{"name": "x"})
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s %s: want 401, got %d", tc.method, tc.path, resp.StatusCode)
		}
	}
}

// ── Area Reorder ──────────────────────────────────────────────────────────────

func TestIntegration_Area_Reorder_FullSet(t *testing.T) {
	srv, pool := newAreaServer(t)
	_, token := mustCreateUser(t, pool, "pro")

	a1 := mustCreateArea(t, srv, token, "First")
	a2 := mustCreateArea(t, srv, token, "Second")
	a3 := mustCreateArea(t, srv, token, "Third")

	body := map[string]any{
		"items": []map[string]any{
			{"id": a1, "displayOrder": 2},
			{"id": a2, "displayOrder": 0},
			{"id": a3, "displayOrder": 1},
		},
	}
	resp := do(t, http.MethodPatch, srv.URL+"/v1/areas/reorder", token, body)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	var env struct {
		Data struct {
			Updated int `json:"updated"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.Updated != 3 {
		t.Errorf("updated: want 3, got %d", env.Data.Updated)
	}

	// Confirm GET reflects the new order.
	listResp := do(t, http.MethodGet, srv.URL+"/v1/areas", token, nil)
	defer listResp.Body.Close()
	var listEnv struct {
		Data struct {
			Items []area.AreaView `json:"items"`
		} `json:"data"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listEnv); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	// a2 should now be first (displayOrder=0).
	if len(listEnv.Data.Items) > 0 && listEnv.Data.Items[0].ID != a2 {
		t.Errorf("after reorder, first item should be a2, got %q", listEnv.Data.Items[0].ID)
	}
}

func TestIntegration_Area_Reorder_PartialSet(t *testing.T) {
	srv, pool := newAreaServer(t)
	_, token := mustCreateUser(t, pool, "pro")

	a1 := mustCreateArea(t, srv, token, "A1")
	a2 := mustCreateArea(t, srv, token, "A2")
	mustCreateArea(t, srv, token, "A3")
	mustCreateArea(t, srv, token, "A4")
	mustCreateArea(t, srv, token, "A5")

	body := map[string]any{
		"items": []map[string]any{
			{"id": a1, "displayOrder": 10},
			{"id": a2, "displayOrder": 20},
		},
	}
	resp := do(t, http.MethodPatch, srv.URL+"/v1/areas/reorder", token, body)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusOK)

	var env struct {
		Data struct {
			Updated int `json:"updated"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if env.Data.Updated != 2 {
		t.Errorf("updated: want 2, got %d", env.Data.Updated)
	}
}

func TestIntegration_Area_Reorder_CrossUserIDRejected(t *testing.T) {
	srv, pool := newAreaServer(t)
	_, tokenA := mustCreateUser(t, pool, "pro")
	_, tokenB := mustCreateUser(t, pool, "pro")

	areaA := mustCreateArea(t, srv, tokenA, "User A Area")
	areaB := mustCreateArea(t, srv, tokenB, "User B Area")

	// User B tries to reorder their area but includes User A's area ID.
	body := map[string]any{
		"items": []map[string]any{
			{"id": areaB, "displayOrder": 0},
			{"id": areaA, "displayOrder": 1}, // foreign
		},
	}
	resp := do(t, http.MethodPatch, srv.URL+"/v1/areas/reorder", tokenB, body)
	defer resp.Body.Close()
	assertStatus(t, resp, http.StatusNotFound)
	assertErrorCode(t, resp, apperror.ErrAreaNotFound)

	// Verify areaB's displayOrder was NOT changed (atomic rollback).
	var order int
	err := pool.QueryRow(context.Background(),
		`SELECT display_order FROM areas WHERE id = $1`, areaB,
	).Scan(&order)
	if err != nil {
		t.Fatalf("query area: %v", err)
	}
	if order != 0 {
		t.Errorf("areaB displayOrder should be unchanged (0), got %d", order)
	}
}
