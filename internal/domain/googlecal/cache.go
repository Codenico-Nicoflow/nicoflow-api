package googlecal

import (
	"strings"
	"sync"
	"time"
)

// cacheTTL is how long a fetched range stays fresh.
//
// Short on purpose. The cache exists to absorb the burst from view switching —
// a user flipping day→week→day fires three identical fetches in as many seconds
// — not to reduce load in general. A longer TTL would start hiding real changes,
// and a meeting rescheduled minutes before it starts is exactly when staleness
// hurts most. Explicit refresh bypasses this entirely.
const cacheTTL = 3 * time.Minute

// eventCache is a per-process TTL cache of ranged event fetches.
//
// Deliberately in-process rather than Redis: no such infrastructure exists, and
// the worst failure mode of this design is a redundant API call after a deploy
// or on a second instance. That is a far better failure than adding a network
// dependency to the read path of a feature whose entire job is to degrade
// gracefully.
//
// Safe for concurrent use.
type eventCache struct {
	mu      sync.Mutex
	entries map[string]cacheEntry
	// now is injectable so TTL behaviour is tested by advancing a clock rather
	// than by sleeping.
	now func() time.Time
}

type cacheEntry struct {
	events    []CalendarEvent
	expiresAt time.Time
}

func newEventCache() *eventCache {
	return &eventCache{entries: map[string]cacheEntry{}, now: time.Now}
}

// cacheKey identifies a fetch by everything that changes its result. Calendar
// IDs are joined with a separator that cannot appear in a Google calendar ID
// (they are email-shaped), so two different selections cannot collide into one
// key.
func cacheKey(userID string, calendarIDs []string, from, to time.Time) string {
	return userID + "\x00" + strings.Join(calendarIDs, "\x00") + "\x00" +
		from.UTC().Format(time.RFC3339) + "\x00" + to.UTC().Format(time.RFC3339)
}

// get returns the cached events for a key when the entry is still fresh.
func (c *eventCache) get(key string) ([]CalendarEvent, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	// An expired entry is dropped rather than left to accumulate — this is the
	// only eviction path, so a key that stops being requested must not pin its
	// events in memory forever.
	if !c.now().Before(entry.expiresAt) {
		delete(c.entries, key)
		return nil, false
	}
	return entry.events, true
}

// put stores events under a key with a fresh TTL. Only successful fetches are
// cached: caching a failure would turn a blip into minutes of forced emptiness.
func (c *eventCache) put(key string, events []CalendarEvent) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = cacheEntry{events: events, expiresAt: c.now().Add(cacheTTL)}
}

// invalidateUser drops every entry belonging to one user. Called when the
// selection changes or the connection is dropped, so a stale overlay cannot
// outlive the connection that produced it.
func (c *eventCache) invalidateUser(userID string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	prefix := userID + "\x00"
	for key := range c.entries {
		if strings.HasPrefix(key, prefix) {
			delete(c.entries, key)
		}
	}
}
