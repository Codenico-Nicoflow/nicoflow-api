package middleware

import (
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/nicoflow/nicoflow-api/internal/apperror"
	"github.com/nicoflow/nicoflow-api/pkg/respond"
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

func newLimiterStore(burst, ratePerMin int) *limiterStore {
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

func rateLimitMiddleware(store *limiterStore, keyFn func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFn(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			l := store.get(key)
			resetAt := time.Now().Add(store.every).Unix()

			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", store.burst))
			w.Header().Set("X-RateLimit-Reset", fmt.Sprintf("%d", resetAt))

			if !l.Allow() {
				retryAfter := int(math.Ceil(store.every.Seconds()))
				w.Header().Set("Retry-After", fmt.Sprintf("%d", retryAfter))
				w.Header().Set("X-RateLimit-Remaining", "0")
				respond.Error(w, http.StatusTooManyRequests, apperror.ErrRateLimited, "too many requests, slow down")
				return
			}

			remaining := int(math.Max(0, l.Tokens()))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", remaining))

			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitIP limits requests by client IP. Intended for public endpoints.
func RateLimitIP(burst, ratePerMin int) func(http.Handler) http.Handler {
	store := newLimiterStore(burst, ratePerMin)
	return rateLimitMiddleware(store, func(r *http.Request) string {
		// Prefer X-Forwarded-For (set by Render's proxy); fall back to RemoteAddr.
		if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
			return ip
		}
		return r.RemoteAddr
	})
}

// RateLimitUser limits requests by authenticated user ID.
// Must be placed after Auth middleware so UserIDFromCtx is populated.
func RateLimitUser(burst, ratePerMin int) func(http.Handler) http.Handler {
	store := newLimiterStore(burst, ratePerMin)
	return rateLimitMiddleware(store, func(r *http.Request) string {
		return UserIDFromCtx(r.Context())
	})
}
