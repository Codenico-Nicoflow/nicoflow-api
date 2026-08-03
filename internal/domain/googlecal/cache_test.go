package googlecal

import (
	"testing"
	"time"
)

// The cache is tested from inside the package: TTL expiry is driven by moving an
// injected clock, since the alternative is a test that sleeps for minutes.

func TestEventCache_ReturnsFreshEntry(t *testing.T) {
	cache := newEventCache()
	key := cacheKey("u1", []string{"primary"}, time.Now(), time.Now().Add(time.Hour))
	cache.put(key, []CalendarEvent{{ID: "evt-1"}})

	got, ok := cache.get(key)
	if !ok {
		t.Fatal("entry missing immediately after put")
	}
	if len(got) != 1 || got[0].ID != "evt-1" {
		t.Errorf("events = %+v", got)
	}
}

func TestEventCache_ExpiresAfterTTL(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	cache := newEventCache()
	cache.now = func() time.Time { return now }

	key := cacheKey("u1", []string{"primary"}, now, now.Add(time.Hour))
	cache.put(key, []CalendarEvent{{ID: "evt-1"}})

	// Just inside the window.
	now = now.Add(cacheTTL - time.Second)
	if _, ok := cache.get(key); !ok {
		t.Error("entry expired before its TTL elapsed")
	}

	// Past it.
	now = now.Add(2 * time.Second)
	if _, ok := cache.get(key); ok {
		t.Error("entry survived past its TTL")
	}
}

// An expired entry is dropped on read, since that is the only eviction path — a
// key nobody asks for again must not pin its events forever.
func TestEventCache_EvictsExpiredEntryOnRead(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	cache := newEventCache()
	cache.now = func() time.Time { return now }

	key := cacheKey("u1", []string{"primary"}, now, now.Add(time.Hour))
	cache.put(key, []CalendarEvent{{ID: "evt-1"}})

	now = now.Add(cacheTTL + time.Second)
	cache.get(key)

	if len(cache.entries) != 0 {
		t.Errorf("entries = %d, want 0 after an expired read", len(cache.entries))
	}
}

// Different selections must not collide — a key built by naive concatenation
// would let {"a","b"} and {"ab"} share an entry.
func TestEventCache_KeysDistinguishSelections(t *testing.T) {
	from := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 1)

	if cacheKey("u1", []string{"a", "b"}, from, to) == cacheKey("u1", []string{"ab"}, from, to) {
		t.Error("distinct calendar selections produced the same key")
	}
	if cacheKey("u1", []string{"primary"}, from, to) == cacheKey("u2", []string{"primary"}, from, to) {
		t.Error("two users share a cache key")
	}
}

func TestEventCache_InvalidateUserDropsOnlyThatUser(t *testing.T) {
	cache := newEventCache()
	from := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 1)

	mine := cacheKey("u1", []string{"primary"}, from, to)
	theirs := cacheKey("u2", []string{"primary"}, from, to)
	cache.put(mine, []CalendarEvent{{ID: "a"}})
	cache.put(theirs, []CalendarEvent{{ID: "b"}})

	cache.invalidateUser("u1")

	if _, ok := cache.get(mine); ok {
		t.Error("invalidated user still cached")
	}
	if _, ok := cache.get(theirs); !ok {
		t.Error("another user's entry was dropped")
	}
}

// A user id that prefixes another's must not take it down with it.
func TestEventCache_InvalidateUserIsNotPrefixMatched(t *testing.T) {
	cache := newEventCache()
	from := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 1)

	short := cacheKey("u1", []string{"primary"}, from, to)
	longer := cacheKey("u10", []string{"primary"}, from, to)
	cache.put(short, []CalendarEvent{{ID: "a"}})
	cache.put(longer, []CalendarEvent{{ID: "b"}})

	cache.invalidateUser("u1")

	if _, ok := cache.get(longer); !ok {
		t.Error("u10's entry was dropped when invalidating u1")
	}
}
