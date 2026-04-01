package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"

	"nicoflow-api/internal/middleware"
	"nicoflow-api/internal/response"
)

const testSecret = "test-secret-key"

// makeToken creates a signed JWT for testing.
// Set expiresAt in the past to produce an expired token.
// Use a different secret to produce a wrong-secret token.
func makeToken(userID, plan, secret string, expiresAt time.Time) string {
	type jwtClaims struct {
		UserID string `json:"userId"`
		Plan   string `json:"plan"`
		jwt.RegisteredClaims
	}
	cl := jwtClaims{
		UserID: userID,
		Plan:   plan,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, cl)
	signed, _ := token.SignedString([]byte(secret))
	return signed
}

// setupAuthRouter returns a Gin engine with the Auth middleware applied.
// The protected handler echoes back the userID set in context.
func setupAuthRouter(secret string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/protected", middleware.Auth(secret), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"user_id": c.GetString(middleware.ContextUserID), "plan": c.GetString(middleware.ContextPlan)})
	})
	return r
}

func TestAuthMiddleware(t *testing.T) {
	tests := []struct {
		name          string
		authHeader    string
		wantStatus    int
		wantErrorCode string
	}{
		{
			name:          "valid token accepted",
			authHeader:    "Bearer " + makeToken("user1", "free", testSecret, time.Now().Add(15*time.Minute)),
			wantStatus:    http.StatusOK,
			wantErrorCode: "",
		},
		{
			name:          "expired token rejected",
			authHeader:    "Bearer " + makeToken("user1", "free", testSecret, time.Now().Add(-1*time.Minute)),
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: response.ErrInvalidToken,
		},
		{
			name:          "wrong secret rejected",
			authHeader:    "Bearer " + makeToken("user1", "free", "wrong-secret", time.Now().Add(15*time.Minute)),
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: response.ErrInvalidToken,
		},
		{
			name:          "missing authorization header",
			authHeader:    "",
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: response.ErrInvalidToken,
		},
		{
			name:          "malformed header — no Bearer prefix",
			authHeader:    makeToken("user1", "free", testSecret, time.Now().Add(15*time.Minute)),
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: response.ErrInvalidToken,
		},
		{
			name:          "missing userID claim",
			authHeader:    "Bearer " + makeToken("", "free", testSecret, time.Now().Add(15*time.Minute)),
			wantStatus:    http.StatusUnauthorized,
			wantErrorCode: response.ErrInvalidToken,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := setupAuthRouter(testSecret)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			r.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("expected status %d, got %d", tc.wantStatus, w.Code)
			}

			var body struct {
				Error struct {
					Code    string `json:"code"`
					Message string `json:"message"`
				} `json:"error"`
			}
			if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
				t.Fatalf("decode body: %v", err)
			}

			if tc.wantErrorCode != body.Error.Code {
				t.Errorf("expected error code %q, got %q", tc.wantErrorCode, body.Error.Code)
			}
		})
	}
}

func TestAuthMiddleware_UserIDInContext(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		wantUserID string
		wantStatus int
	}{
		{
			name:       "valid token sets userID",
			authHeader: "Bearer " + makeToken("user1", "free", testSecret, time.Now().Add(15*time.Minute)),
			wantUserID: "user1",
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid token — no userID in context",
			authHeader: "Bearer invalid-token",
			wantUserID: "",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := setupAuthRouter(testSecret)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			r.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("expected status %d, got %d", tc.wantStatus, w.Code)
			}

			if w.Code == http.StatusOK {
				var body struct {
					UserID string `json:"user_id"`
				}
				if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if body.UserID != tc.wantUserID {
					t.Errorf("expected user_id %q, got %q", tc.wantUserID, body.UserID)
				}
			}
		})
	}
}

func TestAuthMiddleware_PlanInContext(t *testing.T) {
	tests := []struct {
		name       string
		authHeader string
		wantPlan   string
		wantStatus int
	}{
		{
			name:       "valid token sets userID",
			authHeader: "Bearer " + makeToken("user1", "free", testSecret, time.Now().Add(15*time.Minute)),
			wantPlan:   "free",
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid token — no userID in context",
			authHeader: "Bearer invalid-token",
			wantPlan:   "",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := setupAuthRouter(testSecret)
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/protected", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			r.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Errorf("expected status %d, got %d", tc.wantStatus, w.Code)
			}

			if w.Code == http.StatusOK {
				var body struct {
					Plan string `json:"plan"`
				}
				if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				if body.Plan != tc.wantPlan {
					t.Errorf("expected user_id %q, got %q", tc.wantPlan, body.Plan)
				}
			}
		})
	}
}
