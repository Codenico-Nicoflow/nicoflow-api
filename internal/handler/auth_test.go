package handler_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/go-playground/validator/v10"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"nicoflow-api/internal/handler"
	"nicoflow-api/internal/middleware"
	"nicoflow-api/internal/model"
	pgrepo "nicoflow-api/internal/repository/postgres"
	"nicoflow-api/internal/service"
	"nicoflow-api/internal/testutil"
	"nicoflow-api/internal/validations"
)

const authTestSecret = "integration-test-secret"

// setupAuthTestRouter wires the real service + handler against the test DB.
// Returns the router and the pool so tests can insert rows directly.
func setupAuthTestRouter(t *testing.T) (*gin.Engine, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewTestDB(t)
	t.Cleanup(func() {
		testutil.CleanTables(t, pool, "refresh_tokens", "users")
	})

	userRepo := pgrepo.NewUserRepo(pool)
	refreshRepo := pgrepo.NewRefreshTokenRepo(pool)
	planRepo := pgrepo.NewUserPlanRepo(pool)

	svc := service.NewAuthService(userRepo, refreshRepo, planRepo, authTestSecret)
	h := handler.NewAuthHandler(svc)

	if v, ok := binding.Validator.Engine().(*validator.Validate); ok {
		_ = v.RegisterValidation("strong_password", validations.PasswordValidator)
	}

	gin.SetMode(gin.TestMode)
	r := gin.New()

	auth := r.Group("/v1/auth")
	auth.POST("/register", h.Register)
	auth.POST("/login", h.Login)
	auth.POST("/refresh", h.Refresh)

	protected := r.Group("/v1/auth", middleware.Auth(authTestSecret))
	protected.POST("/logout", h.Logout)
	protected.POST("/logout-all", h.LogoutAll)

	return r, pool
}

// jsonBody builds a JSON request body from a map.
func jsonBody(t *testing.T, v map[string]string) *bytes.Buffer {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("jsonBody: %v", err)
	}
	return bytes.NewBuffer(b)
}

// decodeResponseInto decodes the standard {"data":..., "error":...} envelope.
func decodeResponseInto[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(w.Body).Decode(&v); err != nil {
		t.Fatalf("decodeInto: %v", err)
	}
	return v
}

type tokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

type tokenPairEnvelope struct {
	Data tokenPair `json:"data"`
}

// registerUser registers a user and returns the response recorder.
func registerUser(t *testing.T, r *gin.Engine, email, password string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/register", jsonBody(t, map[string]string{
		"email":    email,
		"password": password,
	}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// loginUser logs in and returns both tokens. Fails the test if login doesn't return 200.
func loginUser(t *testing.T, r *gin.Engine, email, password string) tokenPair {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", jsonBody(t, map[string]string{
		"email":    email,
		"password": password,
	}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("loginUser: expected 200, got %d — body: %s", w.Code, w.Body.String())
	}
	res := decodeResponseInto[tokenPairEnvelope](t, w)
	return res.Data
}

// hashRefreshToken mirrors the service's hashToken function for test use.
func hashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return "rt_" + hex.EncodeToString(sum[:])
}

// insertExpiredRefreshToken inserts a refresh token with expires_at in the past.
func insertExpiredRefreshToken(t *testing.T, pool *pgxpool.Pool, userID, rawToken string) {
	t.Helper()
	tok := &model.RefreshToken{
		ID:        uuid.NewString(),
		UserID:    userID,
		TokenHash: hashRefreshToken(rawToken),
		ExpiresAt: time.Now().Add(-1 * time.Hour),
		CreatedAt: time.Now(),
	}
	_, err := pool.Exec(context.Background(),
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		tok.ID, tok.UserID, tok.TokenHash, tok.ExpiresAt, tok.CreatedAt,
	)
	if err != nil {
		t.Fatalf("insertExpiredRefreshToken: %v", err)
	}
}

// getUserID fetches the user_id from the users table by email.
func getUserID(t *testing.T, pool *pgxpool.Pool, email string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `SELECT id FROM users WHERE email = $1`, email).Scan(&id)
	if err != nil {
		t.Fatalf("getUserID: %v", err)
	}
	return id
}

// ── Register ─────────────────────────────────────────────────────────────────

func TestRegister_HappyPath(t *testing.T) {
	r, _ := setupAuthTestRouter(t)

	w := registerUser(t, r, "test@example.com", "StrongPass123!")
	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — body: %s", w.Code, w.Body.String())
	}

	res := decodeResponseInto[struct {
		Data struct {
			ID    string `json:"id"`
			Email string `json:"email"`
		} `json:"data"`
	}](t, w)

	if res.Data.ID == "" {
		t.Fatal("expected non-empty user ID")
	}
	if res.Data.Email != "test@example.com" {
		t.Errorf("expected email %q, got %q", "test@example.com", res.Data.Email)
	}
}

func TestRegister_DuplicateEmail(t *testing.T) {
	r, _ := setupAuthTestRouter(t)

	w1 := registerUser(t, r, "duplicate@example.com", "StrongPass123!")
	if w1.Code != http.StatusCreated {
		t.Fatalf("first register: expected 201, got %d", w1.Code)
	}

	w2 := registerUser(t, r, "duplicate@example.com", "StrongPass123!")
	if w2.Code != http.StatusConflict {
		t.Fatalf("second register: expected 409, got %d", w2.Code)
	}
}

func TestRegister_WeakPassword(t *testing.T) {
	r, _ := setupAuthTestRouter(t)

	w := registerUser(t, r, "test@example.com", "weakpassword")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestRegister_InvalidEmail(t *testing.T) {
	r, _ := setupAuthTestRouter(t)

	w := registerUser(t, r, "notanemail", "StrongPassword123!")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

// ── Login ─────────────────────────────────────────────────────────────────────

func TestLogin_HappyPath(t *testing.T) {
	r, _ := setupAuthTestRouter(t)

	w := registerUser(t, r, "test@example.com", "StrongPass123!")
	if w.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", w.Code)
	}

	tokens := loginUser(t, r, "test@example.com", "StrongPass123!")
	if tokens.AccessToken == "" {
		t.Error("expected non-empty access_token")
	}
	if tokens.RefreshToken == "" {
		t.Error("expected non-empty refresh_token")
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	r, _ := setupAuthTestRouter(t)

	w := registerUser(t, r, "test@example.com", "StrongPass123!")
	if w.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", w.Code)
	}

	wLogin := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", jsonBody(t, map[string]string{
		"email":    "test@example.com",
		"password": "WrongPass123!",
	}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(wLogin, req)
	if wLogin.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", wLogin.Code)
	}
}

func TestLogin_UnknownEmail(t *testing.T) {
	r, _ := setupAuthTestRouter(t)

	wLogin := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", jsonBody(t, map[string]string{
		"email":    "unknown@example.com",
		"password": "StrongPass123!",
	}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(wLogin, req)
	if wLogin.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", wLogin.Code)
	}
}

// ── Refresh ───────────────────────────────────────────────────────────────────

func TestRefresh_HappyPath(t *testing.T) {
	r, _ := setupAuthTestRouter(t)

	w := registerUser(t, r, "test@example.com", "StrongPass123!")
	if w.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", w.Code)
	}
	tokens := loginUser(t, r, "test@example.com", "StrongPass123!")

	wRefresh := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", jsonBody(t, map[string]string{
		"refresh_token": tokens.RefreshToken,
	}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(wRefresh, req)
	if wRefresh.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", wRefresh.Code, wRefresh.Body.String())
	}

	res := decodeResponseInto[tokenPairEnvelope](t, wRefresh)
	if res.Data.AccessToken == "" {
		t.Error("expected non-empty access_token after refresh")
	}
	if res.Data.RefreshToken == "" {
		t.Error("expected non-empty refresh_token after refresh")
	}
}

func TestRefresh_TokenReplay(t *testing.T) {
	r, _ := setupAuthTestRouter(t)

	w := registerUser(t, r, "test@example.com", "StrongPass123!")
	if w.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", w.Code)
	}
	tokens := loginUser(t, r, "test@example.com", "StrongPass123!")

	// First use — should succeed
	w1 := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", jsonBody(t, map[string]string{
		"refresh_token": tokens.RefreshToken,
	}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w1, req)
	if w1.Code != http.StatusOK {
		t.Fatalf("first refresh: expected 200, got %d", w1.Code)
	}

	// Second use of same token — should fail (single-use rotation)
	w2 := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", jsonBody(t, map[string]string{
		"refresh_token": tokens.RefreshToken,
	}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req)
	if w2.Code != http.StatusUnauthorized {
		t.Fatalf("replay: expected 401, got %d", w2.Code)
	}
}

func TestRefresh_ExpiredToken(t *testing.T) {
	r, pool := setupAuthTestRouter(t)

	w := registerUser(t, r, "test@example.com", "StrongPass123!")
	if w.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", w.Code)
	}
	userID := getUserID(t, pool, "test@example.com")

	rawToken := "expiredrawtoken_" + uuid.NewString()
	insertExpiredRefreshToken(t, pool, userID, rawToken)

	wRefresh := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", jsonBody(t, map[string]string{
		"refresh_token": rawToken,
	}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(wRefresh, req)
	if wRefresh.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d — body: %s", wRefresh.Code, wRefresh.Body.String())
	}
}

func TestRefresh_InvalidToken(t *testing.T) {
	r, _ := setupAuthTestRouter(t)

	wRefresh := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", jsonBody(t, map[string]string{
		"refresh_token": "completelyrandomtokenthatdoesnotexist",
	}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(wRefresh, req)
	if wRefresh.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", wRefresh.Code)
	}
}

// ── Logout ────────────────────────────────────────────────────────────────────

func TestLogout_HappyPath(t *testing.T) {
	r, _ := setupAuthTestRouter(t)

	w := registerUser(t, r, "test@example.com", "StrongPass123!")
	if w.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", w.Code)
	}
	tokens := loginUser(t, r, "test@example.com", "StrongPass123!")

	// Logout with the refresh token
	wLogout := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", jsonBody(t, map[string]string{
		"refresh_token": tokens.RefreshToken,
	}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	r.ServeHTTP(wLogout, req)
	if wLogout.Code != http.StatusOK {
		t.Fatalf("logout: expected 200, got %d — body: %s", wLogout.Code, wLogout.Body.String())
	}

	// Refresh with same token after logout — should fail
	wRefresh := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", jsonBody(t, map[string]string{
		"refresh_token": tokens.RefreshToken,
	}))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(wRefresh, req)
	if wRefresh.Code != http.StatusUnauthorized {
		t.Fatalf("post-logout refresh: expected 401, got %d", wRefresh.Code)
	}
}

func TestLogout_UnknownToken(t *testing.T) {
	r, _ := setupAuthTestRouter(t)

	w := registerUser(t, r, "test@example.com", "StrongPass123!")
	if w.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", w.Code)
	}
	tokens := loginUser(t, r, "test@example.com", "StrongPass123!")

	wLogout := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", jsonBody(t, map[string]string{
		"refresh_token": "tokenthatisnot_in_db",
	}))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	r.ServeHTTP(wLogout, req)
	// idempotent — unknown token is not an error
	if wLogout.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d — body: %s", wLogout.Code, wLogout.Body.String())
	}
}

// ── LogoutAll ─────────────────────────────────────────────────────────────────

func TestLogoutAll(t *testing.T) {
	r, _ := setupAuthTestRouter(t)

	w := registerUser(t, r, "test@example.com", "StrongPass123!")
	if w.Code != http.StatusCreated {
		t.Fatalf("register: expected 201, got %d", w.Code)
	}

	// Two separate logins (two sessions)
	tokens1 := loginUser(t, r, "test@example.com", "StrongPass123!")
	tokens2 := loginUser(t, r, "test@example.com", "StrongPass123!")

	// Logout all using first session's access token
	wLogoutAll := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/logout-all", nil)
	req.Header.Set("Authorization", "Bearer "+tokens1.AccessToken)
	r.ServeHTTP(wLogoutAll, req)
	if wLogoutAll.Code != http.StatusOK {
		t.Fatalf("logout-all: expected 200, got %d — body: %s", wLogoutAll.Code, wLogoutAll.Body.String())
	}

	// Both refresh tokens should now be invalid
	for i, rt := range []string{tokens1.RefreshToken, tokens2.RefreshToken} {
		wRefresh := httptest.NewRecorder()
		req = httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", jsonBody(t, map[string]string{
			"refresh_token": rt,
		}))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(wRefresh, req)
		if wRefresh.Code != http.StatusUnauthorized {
			t.Errorf("session %d: expected 401 after logout-all, got %d", i+1, wRefresh.Code)
		}
	}
}
