package middleware

import (
	"fmt"
	"math"
	"net"
	"net/http"
	"strings"
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
	stop    chan struct{}
}

func newLimiterStore(burst, ratePerMin int) *limiterStore {
	s := &limiterStore{
		entries: make(map[string]*limiterEntry),
		burst:   burst,
		every:   time.Minute / time.Duration(ratePerMin),
		stop:    make(chan struct{}),
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
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			s.mu.Lock()
			for k, e := range s.entries {
				if time.Since(e.lastSeen) > 5*time.Minute {
					delete(s.entries, k)
				}
			}
			s.mu.Unlock()
		case <-s.stop:
			return
		}
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
//
// trustedProxies is a list of CIDR ranges (e.g. "10.0.0.0/8") whose
// X-Forwarded-For header can be trusted. When the request's RemoteAddr is
// within one of those ranges, we use the leftmost IP in X-Forwarded-For as
// the rate-limit key. When trustedProxies is empty, X-Forwarded-For is always
// used as-is (safe for single-proxy deployments like Render where all traffic
// passes through the proxy). In both cases RemoteAddr has its port stripped.
func RateLimitIP(burst, ratePerMin int, trustedProxies []string) func(http.Handler) http.Handler {
	nets := parseCIDRs(trustedProxies)
	store := newLimiterStore(burst, ratePerMin)
	return rateLimitMiddleware(store, func(r *http.Request) string {
		return clientIP(r, nets)
	})
}

// clientIP returns the rate-limit key IP for the request.
func clientIP(r *http.Request, trustedNets []*net.IPNet) string {
	remoteHost, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remoteHost = r.RemoteAddr
	}

	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return remoteHost
	}

	// If trusted proxies are configured, only trust XFF when RemoteAddr is within one.
	if len(trustedNets) > 0 {
		remoteIP := net.ParseIP(remoteHost)
		trusted := false
		for _, n := range trustedNets {
			if remoteIP != nil && n.Contains(remoteIP) {
				trusted = true
				break
			}
		}
		if !trusted {
			return remoteHost
		}
	}

	// Use only the leftmost (real client) IP from X-Forwarded-For.
	return strings.TrimSpace(strings.SplitN(xff, ",", 2)[0])
}

// parseCIDRs parses a slice of CIDR strings, silently skipping invalid ones.
func parseCIDRs(cidrs []string) []*net.IPNet {
	nets := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		_, n, err := net.ParseCIDR(c)
		if err == nil {
			nets = append(nets, n)
		}
	}
	return nets
}

// RateLimitUser limits requests by authenticated user ID.
// Must be placed after Auth middleware so UserIDFromCtx is populated.
func RateLimitUser(burst, ratePerMin int) func(http.Handler) http.Handler {
	store := newLimiterStore(burst, ratePerMin)
	return rateLimitMiddleware(store, func(r *http.Request) string {
		return UserIDFromCtx(r.Context())
	})
}
