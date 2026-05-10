package middleware

import (
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"

	"github.com/nicoflow/nicoflow-api/internal/response"
)

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

type limiterStore struct {
	mu      sync.Mutex
	entries map[string]*limiterEntry
	burst   int
	every   time.Duration
}

func newLimiterStore(burst int, ratePerMin int) *limiterStore {
	s := &limiterStore{
		entries: make(map[string]*limiterEntry),
		burst:   burst,
		every:   time.Minute / time.Duration(ratePerMin),
	}
	go s.cleanup()
	return s
}

func (s *limiterStore) get(key string) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[key]
	if !ok {
		e = &limiterEntry{limiter: rate.NewLimiter(rate.Every(s.every), s.burst)}
		s.entries[key] = e
	}
	e.lastSeen = time.Now()
	return e.limiter
}

func (s *limiterStore) cleanup() {
	for {
		time.Sleep(5 * time.Minute)
		s.mu.Lock()
		for k, e := range s.entries {
			if time.Since(e.lastSeen) > 5*time.Minute {
				delete(s.entries, k)
			}
		}
		s.mu.Unlock()
	}
}

func rateLimitMiddleware(store *limiterStore, keyFn func(*gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := keyFn(c)
		if key == "" {
			c.Next()
			return
		}

		l := store.get(key)
		resetAt := time.Now().Add(store.every).Unix()

		c.Header("X-RateLimit-Limit", fmt.Sprintf("%d", store.burst))
		c.Header("X-RateLimit-Reset", fmt.Sprintf("%d", resetAt))

		if !l.Allow() {
			retryAfter := int(math.Ceil(store.every.Seconds()))
			c.Header("Retry-After", fmt.Sprintf("%d", retryAfter))
			c.Header("X-RateLimit-Remaining", "0")
			response.RespondError(c, http.StatusTooManyRequests, response.ErrRateLimited, "too many requests, slow down")
			return
		}

		remaining := int(math.Max(0, l.Tokens()))
		c.Header("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))

		c.Next()
	}
}

// RateLimitIP limits requests by client IP address.
// Intended for public endpoints like /auth/* where no user ID is available.
func RateLimitIP(burst int, ratePerMin int) gin.HandlerFunc {
	store := newLimiterStore(burst, ratePerMin)
	return rateLimitMiddleware(store, func(c *gin.Context) string {
		return c.ClientIP()
	})
}

// RateLimitUser limits requests by authenticated user ID.
// Must be placed after the Auth middleware so ContextUserID is populated.
func RateLimitUser(burst int, ratePerMin int) gin.HandlerFunc {
	store := newLimiterStore(burst, ratePerMin)
	return rateLimitMiddleware(store, func(c *gin.Context) string {
		return c.GetString(ContextUserID)
	})
}
