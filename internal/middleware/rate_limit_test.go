package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/nicoflow/nicoflow-api/internal/middleware"
	"github.com/nicoflow/nicoflow-api/internal/response"
)

func setupAuthRateLimitRouter(burst, ratePerMin int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(middleware.RateLimitIP(burst, ratePerMin))
	r.POST("/auth/login", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

func setupUserRateLimitRouter(burst, ratePerMin int, userID string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Inject userID into context the same way Auth middleware does
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextUserID, userID)
		c.Next()
	})
	r.Use(middleware.RateLimitUser(burst, ratePerMin))
	r.GET("/tasks", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

func setupMultiUserRateLimitRouter(burst, ratePerMin int) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set(middleware.ContextUserID, c.GetHeader("X-Test-User-ID"))
		c.Next()
	})
	r.Use(middleware.RateLimitUser(burst, ratePerMin))
	r.GET("/tasks", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

func TestRateLimitIP_BlocksAfterBurst(t *testing.T) {
	const burst = 3
	r := setupAuthRateLimitRouter(burst, burst)

	// First `burst` requests should all succeed
	for i := range burst {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
		if w.Header().Get("X-RateLimit-Limit") == "" {
			t.Errorf("request %d: missing X-RateLimit-Limit header", i+1)
		}
	}

	// The (burst+1)th request must be blocked
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header on 429 response")
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body.Error.Code != response.ErrRateLimited {
		t.Errorf("expected error code %s, got %q", response.ErrRateLimited, body.Error.Code)
	}
}

// TODO: implement these tests following the same pattern above:

func TestRateLimitIP_HeadersPresent(t *testing.T) {
	const burst = 3
	r := setupAuthRateLimitRouter(burst, burst)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	r.ServeHTTP(w, req)

	if w.Header().Get("X-RateLimit-Limit") == "" {
		t.Error("expected X-RateLimit-Limit header")
	}
	if w.Header().Get("X-RateLimit-Remaining") == "" {
		t.Error("expected X-RateLimit-Remaining header")
	}
	if w.Header().Get("X-RateLimit-Reset") == "" {
		t.Error("expected X-RateLimit-Reset header")
	}

	rateLimitRemainingInt, err := strconv.Atoi(w.Header().Get("X-RateLimit-Remaining"))
	if err != nil {
		t.Fatalf("expected X-RateLimit-Remaining to be a valid integer, got %q", w.Header().Get("X-RateLimit-Remaining"))
	}

	rateLimitInt, err := strconv.Atoi(w.Header().Get("X-RateLimit-Limit"))
	if err != nil {
		t.Fatalf("expected X-RateLimit-Limit to be a valid integer, got %q", w.Header().Get("X-RateLimit-Limit"))
	}

	if rateLimitRemainingInt >= rateLimitInt {
		t.Errorf("expected X-RateLimit-Remaining to be less than X-RateLimit-Limit, got %d >= %d", rateLimitRemainingInt, rateLimitInt)
	}

}

func TestRateLimitIP_DifferentIPs(t *testing.T) {
	const burst = 10
	ips := []string{"1.2.3.4", "5.6.7.8"}
	r := setupAuthRateLimitRouter(burst, burst)

	for i := range burst {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		req.Header.Set("X-Forwarded-For", ips[0])
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected status OK, got %d", i+1, w.Code)
		}
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.Header.Set("X-Forwarded-For", ips[0])
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected status Too Many Requests after burst, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.Header.Set("X-Forwarded-For", ips[1])
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status OK after burst for different IP, got %d", w.Code)
	}

	for i := range burst - 1 {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/auth/login", nil)
		req.Header.Set("X-Forwarded-For", ips[1])
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("request %d: expected status OK after burst for same IP, got %d", i+1, w.Code)
		}
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/auth/login", nil)
	req.Header.Set("X-Forwarded-For", ips[1])
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Errorf("expected status Too Many Requests after burst for same IP, got %d", w.Code)
	}
}

func TestRateLimitUser_BlocksAfterBurst(t *testing.T) {
	const burst = 3
	r := setupUserRateLimitRouter(burst, burst, "user1")

	// First `burst` requests should all succeed
	for i := range burst {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/tasks", nil)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
		if w.Header().Get("X-RateLimit-Limit") == "" {
			t.Errorf("request %d: missing X-RateLimit-Limit header", i+1)
		}
	}

	// The (burst+1)th request must be blocked
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}
	if w.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header on 429 response")
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body.Error.Code != response.ErrRateLimited {
		t.Errorf("expected error code %s, got %q", response.ErrRateLimited, body.Error.Code)
	}
}

func TestRateLimitUser_DIfferentUser(t *testing.T) {
	const userOne = "user1"
	const userTwo = "user2"
	const burst = 10

	r := setupMultiUserRateLimitRouter(burst, burst)

	for i := range burst {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/tasks", nil)
		req.Header.Set("X-Test-User-ID", userOne)
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, w.Code)
		}
		if w.Header().Get("X-RateLimit-Limit") == "" {
			t.Errorf("request %d: missing X-RateLimit-Limit header", i+1)
		}
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/tasks", nil)
	req.Header.Set("X-Test-User-ID", userOne)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", w.Code)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/tasks", nil)
	req.Header.Set("X-Test-User-ID", userTwo)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

// TestRateLimitUser_DifferentUsers — two different userIDs should have
// independent buckets (one exhausted should not affect the other).
