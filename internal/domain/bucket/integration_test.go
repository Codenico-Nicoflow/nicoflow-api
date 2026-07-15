//go:build integration

package bucket_test

import (
	"bytes"
	"context"
	"encoding/json"
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
	"github.com/nicoflow/nicoflow-api/internal/domain/notification"
	"github.com/nicoflow/nicoflow-api/internal/domain/task"
	"github.com/nicoflow/nicoflow-api/internal/handler"
	"github.com/nicoflow/nicoflow-api/internal/testutil"
	"github.com/nicoflow/nicoflow-api/pkg/jwtutil"
)

const (
	integrationJWTSecret = "integration-test-secret-32-bytes!!"
	testEmailDomain      = "@bucket-integration.test"
)

type bucketEnv struct {
	srv       *httptest.Server
	pool      *pgxpool.Pool
	token     string
	userID    string
	projectID string
}

func cleanBucketTestData(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	queries := []string{
		`DELETE FROM notifications WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%` + testEmailDomain + `')`,
		`DELETE FROM bucket   WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%` + testEmailDomain + `')`,
		`DELETE FROM tasks    WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%` + testEmailDomain + `')`,
		`DELETE FROM projects WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%` + testEmailDomain + `')`,
		`DELETE FROM areas    WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%` + testEmailDomain + `')`,
		`DELETE FROM refresh_tokens WHERE user_id IN (SELECT id FROM users WHERE email LIKE '%` + testEmailDomain + `')`,
		`DELETE FROM users WHERE email LIKE '%` + testEmailDomain + `'`,
	}
	for _, q := range queries {
		if _, err := pool.Exec(context.Background(), q); err != nil {
			t.Fatalf("cleanBucketTestData: %v", err)
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

func newBucketServer(t *testing.T, plan string) bucketEnv {
	t.Helper()
	pool := testutil.NewTestDB(t)
	cleanBucketTestData(t, pool)
	t.Cleanup(func() { cleanBucketTestData(t, pool) })

	cfg := config.Config{
		JWTSecret:          integrationJWTSecret,
		JWTExpiry:          15 * time.Minute,
		RefreshTokenExpiry: 7 * 24 * time.Hour,
	}

	email := "user-" + sanitizeEmail(t.Name()) + "-" + plan + testEmailDomain
	userID, token := insertUser(t, pool, email, plan)

	// Real notification service so bucket's inbox_zero path (and CountUnprocessed)
	// runs end-to-end; broadcaster nil (poll-only in this epic).
	notifSvc := notification.NewService(notification.NewRepository(pool), nil)
	taskSvc := task.NewService(task.NewRepository(pool), notifSvc)
	h := handler.Handlers{
		Auth:    auth.NewHandler(auth.NewService(auth.NewRepository(pool), cfg), auth.HandlerConfig{}),
		Area:    area.NewHandler(area.NewService(area.NewRepository(pool))),
		Project: project.NewHandler(project.NewService(project.NewRepository(pool))),
		Task:    task.NewHandler(taskSvc, task.NewSubtaskService(task.NewSubtaskRepository(pool))),
		Bucket:  bucket.NewHandler(bucket.NewService(bucket.NewRepository(pool), taskSvc, notifSvc)),
		AI:      ai.NewHandler(ai.NewService(ai.NewRepository(pool))),
		Billing: billing.NewHandler(billing.NewService(billing.NewRepository(pool))),
	}
	srv := httptest.NewServer(handler.New(cfg, pool, h))
	t.Cleanup(srv.Close)

	areaID := createArea(t, srv, token)
	projectID := createProject(t, srv, token, areaID)
	return bucketEnv{srv: srv, pool: pool, token: token, userID: userID, projectID: projectID}
}

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

func createBucket(t *testing.T, env bucketEnv, content string) string {
	t.Helper()
	resp := do(t, env.srv, http.MethodPost, "/v1/bucket", map[string]any{"content": content}, env.token)
	assertStatus(t, resp, http.StatusCreated)
	var env2 struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	decode(t, resp, &env2)
	return env2.Data.ID
}

// ── tests ───────────────────────────────────────────────────────────────────────

func TestBucket_ProcessToTask_CreatesTaskAndArchives(t *testing.T) {
	env := newBucketServer(t, "free")
	id := createBucket(t, env, "write the report")

	resp := do(t, env.srv, http.MethodPost, "/v1/bucket/"+id+"/process", map[string]any{
		"processingResult": "task",
		"projectId":        env.projectID,
		"taskDetails":      map[string]any{"title": "Write report", "priority": "high"},
	}, env.token)
	assertStatus(t, resp, http.StatusOK)

	var env2 struct {
		Data struct {
			ProcessingResult *string `json:"processingResult"`
			CreatedTaskID    *string `json:"createdTaskId"`
			CreatedNoteID    *string `json:"createdNoteId"`
			ProcessedAt      *string `json:"processedAt"`
		} `json:"data"`
	}
	decode(t, resp, &env2)
	if env2.Data.ProcessingResult == nil || *env2.Data.ProcessingResult != "task" {
		t.Fatalf("processingResult = %v, want task", env2.Data.ProcessingResult)
	}
	if env2.Data.CreatedTaskID == nil {
		t.Fatal("createdTaskId must be set")
	}
	if env2.Data.CreatedNoteID != nil {
		t.Errorf("createdNoteId must be null, got %v", *env2.Data.CreatedNoteID)
	}
	if env2.Data.ProcessedAt == nil {
		t.Error("processedAt must be set")
	}

	// The real task exists in the project.
	taskID := *env2.Data.CreatedTaskID
	tResp := do(t, env.srv, http.MethodGet, "/v1/tasks/"+taskID, nil, env.token)
	assertStatus(t, tResp, http.StatusOK)
	var tEnv struct {
		Data struct {
			Title     string `json:"title"`
			ProjectID string `json:"projectId"`
			Status    string `json:"status"`
			Energy    string `json:"energy"`
		} `json:"data"`
	}
	decode(t, tResp, &tEnv)
	if tEnv.Data.Title != "Write report" || tEnv.Data.ProjectID != env.projectID {
		t.Errorf("task mismatch: %+v", tEnv.Data)
	}
	if tEnv.Data.Status == "" || tEnv.Data.Energy == "" {
		t.Errorf("task defaults (status/energy) not applied: %+v", tEnv.Data)
	}
}

func TestBucket_Create_ContentTooLong(t *testing.T) {
	env := newBucketServer(t, "free")
	resp := do(t, env.srv, http.MethodPost, "/v1/bucket", map[string]any{"content": strings.Repeat("a", 501)}, env.token)
	assertStatus(t, resp, http.StatusUnprocessableEntity)
	assertErrCode(t, resp, "INVALID_INPUT")
}

func TestBucket_RowIsolation_CrossUser404(t *testing.T) {
	env := newBucketServer(t, "free")
	id := createBucket(t, env, "mine")

	_, otherToken := insertUser(t, env.pool, "other-"+sanitizeEmail(t.Name())+testEmailDomain, "free")
	resp := do(t, env.srv, http.MethodGet, "/v1/bucket/"+id, nil, otherToken)
	assertStatus(t, resp, http.StatusNotFound)
	assertErrCode(t, resp, "RESOURCE_NOT_FOUND")
}

func TestBucket_PatchAfterProcess_409(t *testing.T) {
	env := newBucketServer(t, "free")
	id := createBucket(t, env, "capture")
	resp := do(t, env.srv, http.MethodPost, "/v1/bucket/"+id+"/process", map[string]any{"processingResult": "trash"}, env.token)
	assertStatus(t, resp, http.StatusOK)

	edit := do(t, env.srv, http.MethodPatch, "/v1/bucket/"+id, map[string]any{"content": "edited"}, env.token)
	assertStatus(t, edit, http.StatusConflict)
	assertErrCode(t, edit, "CONFLICT")
}

func TestBucket_ProcessAlreadyProcessed_409(t *testing.T) {
	env := newBucketServer(t, "free")
	id := createBucket(t, env, "capture")
	first := do(t, env.srv, http.MethodPost, "/v1/bucket/"+id+"/process", map[string]any{"processingResult": "trash"}, env.token)
	assertStatus(t, first, http.StatusOK)

	second := do(t, env.srv, http.MethodPost, "/v1/bucket/"+id+"/process", map[string]any{
		"processingResult": "task", "projectId": env.projectID, "taskDetails": map[string]any{"title": "x"},
	}, env.token)
	assertStatus(t, second, http.StatusConflict)
	assertErrCode(t, second, "CONFLICT")
}

func TestBucket_DeleteProcessed_204(t *testing.T) {
	env := newBucketServer(t, "free")
	id := createBucket(t, env, "capture")
	do(t, env.srv, http.MethodPost, "/v1/bucket/"+id+"/process", map[string]any{"processingResult": "trash"}, env.token).Body.Close()

	resp := do(t, env.srv, http.MethodDelete, "/v1/bucket/"+id, nil, env.token)
	assertStatus(t, resp, http.StatusNoContent)
	resp.Body.Close()
}

func TestBucket_ProcessNote_501(t *testing.T) {
	env := newBucketServer(t, "free")
	id := createBucket(t, env, "someday idea")
	resp := do(t, env.srv, http.MethodPost, "/v1/bucket/"+id+"/process", map[string]any{"processingResult": "note"}, env.token)
	assertStatus(t, resp, http.StatusNotImplemented)
}

// The NFR under real HTTP: at the free 50-live-task cap, processing to a task
// must 403 AND leave the bucket item unprocessed (still editable).
func TestBucket_ProcessToTask_PlanCap_403_LeavesUnprocessed(t *testing.T) {
	env := newBucketServer(t, "free")

	// Fill the project to the 50 active/inbox cap directly via the API.
	for i := 0; i < 50; i++ {
		r := do(t, env.srv, http.MethodPost, "/v1/projects/"+env.projectID+"/tasks", map[string]any{"title": "t"}, env.token)
		assertStatus(t, r, http.StatusCreated)
		r.Body.Close()
	}

	id := createBucket(t, env, "one more")
	resp := do(t, env.srv, http.MethodPost, "/v1/bucket/"+id+"/process", map[string]any{
		"processingResult": "task", "projectId": env.projectID, "taskDetails": map[string]any{"title": "over cap"},
	}, env.token)
	assertStatus(t, resp, http.StatusForbidden)
	assertErrCode(t, resp, "PLAN_LIMIT_EXCEEDED")

	// The item must still be unprocessed → an edit succeeds (would 409 if marked).
	edit := do(t, env.srv, http.MethodPatch, "/v1/bucket/"+id, map[string]any{"content": "still editable"}, env.token)
	assertStatus(t, edit, http.StatusOK)
	edit.Body.Close()
}

func countNotifications(t *testing.T, env bucketEnv, typ string) int {
	t.Helper()
	var n int
	if err := env.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND type = $2`,
		env.userID, typ).Scan(&n); err != nil {
		t.Fatalf("count notifications: %v", err)
	}
	return n
}

// A Pro user clearing their last inbox item triggers an inbox_zero notification
// (exercises CountUnprocessed end-to-end); a free user in the same flow gets none.
func TestBucket_InboxZero_ProOnly(t *testing.T) {
	pro := newBucketServer(t, "pro")
	id := createBucket(t, pro, "last thing")
	do(t, pro.srv, http.MethodPost, "/v1/bucket/"+id+"/process",
		map[string]any{"processingResult": "trash"}, pro.token).Body.Close()
	if got := countNotifications(t, pro, notification.TypeInboxZero); got != 1 {
		t.Fatalf("pro inbox_zero count = %d, want 1", got)
	}

	free := newBucketServer(t, "free")
	fid := createBucket(t, free, "last thing")
	do(t, free.srv, http.MethodPost, "/v1/bucket/"+fid+"/process",
		map[string]any{"processingResult": "trash"}, free.token).Body.Close()
	if got := countNotifications(t, free, notification.TypeInboxZero); got != 0 {
		t.Fatalf("free inbox_zero count = %d, want 0 (Pro-only)", got)
	}
}
