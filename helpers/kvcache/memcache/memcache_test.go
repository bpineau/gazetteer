package memcache_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/helpers/kvcache"
	"github.com/bpineau/gazetteer/helpers/kvcache/kvcachetest"
	"github.com/bpineau/gazetteer/helpers/kvcache/memcache"
)

func TestSuite(t *testing.T) {
	kvcachetest.Suite(t, func(*testing.T) kvcache.Cache {
		return memcache.New()
	})
}

// TestCapEvictsLeastRecentlyUsed: at the ceiling, a new key costs the
// least-recently-used row and nothing else. Before the cap existed, the map
// grew for the process lifetime and no caller ever called DeleteExpired.
func TestCapEvictsLeastRecentlyUsed(t *testing.T) {
	ctx := context.Background()
	c := memcache.New(memcache.WithMaxEntries(2))

	mustSet(t, c, "a", nil)
	mustSet(t, c, "b", nil)

	// Touch "a" so "b" becomes the least-recently-used row.
	if _, err := c.Get(ctx, "a"); err != nil {
		t.Fatalf("Get(a): %v", err)
	}
	mustSet(t, c, "c", nil)

	if _, err := c.Get(ctx, "b"); !errors.Is(err, kvcache.ErrNotFound) {
		t.Errorf("Get(b) err = %v, want ErrNotFound (b was the LRU row)", err)
	}
	for _, k := range []string{"a", "c"} {
		if _, err := c.Get(ctx, k); err != nil {
			t.Errorf("Get(%s) = %v, want the row to have survived", k, err)
		}
	}
}

// TestCapSweepsExpiredBeforeEvictingLive: under pressure the cache drops dead
// weight first. The expired row here is the MOST recently used one, so a plain
// LRU eviction would have taken the live row instead.
func TestCapSweepsExpiredBeforeEvictingLive(t *testing.T) {
	ctx := context.Background()
	c := memcache.New(memcache.WithMaxEntries(2))

	past := time.Now().Add(-time.Hour)
	mustSet(t, c, "live", nil)
	mustSet(t, c, "expired", &past)

	mustSet(t, c, "fresh", nil)

	if _, err := c.Get(ctx, "expired"); !errors.Is(err, kvcache.ErrNotFound) {
		t.Errorf("Get(expired) err = %v, want ErrNotFound (the sweep runs first)", err)
	}
	for _, k := range []string{"live", "fresh"} {
		if _, err := c.Get(ctx, k); err != nil {
			t.Errorf("Get(%s) = %v, want the row to have survived the sweep", k, err)
		}
	}
}

// TestExpiredRowsSurviveBelowTheCeiling pins the other half of the contract:
// the sweep is pressure-triggered, never a timer. A cache with room to spare
// still serves an expired row, which is what stale-while-revalidate callers
// read (kvcache.Cache.Get's documented semantics).
func TestExpiredRowsSurviveBelowTheCeiling(t *testing.T) {
	ctx := context.Background()
	c := memcache.New() // DefaultMaxEntries, nowhere near full

	past := time.Now().Add(-time.Hour)
	mustSet(t, c, "stale", &past)
	mustSet(t, c, "other", nil)

	got, err := c.Get(ctx, "stale")
	if err != nil {
		t.Fatalf("Get(stale) = %v, want the expired row back", err)
	}
	if got.ExpiresAt == nil || got.ExpiresAt.After(time.Now()) {
		t.Errorf("Get(stale).ExpiresAt = %v, want the past instant it was stored with", got.ExpiresAt)
	}
}

// TestUnlimitedWhenMaxEntriesNonPositive: WithMaxEntries(0) restores the
// unbounded behaviour for callers whose key space is known-bounded.
func TestUnlimitedWhenMaxEntriesNonPositive(t *testing.T) {
	ctx := context.Background()
	c := memcache.New(memcache.WithMaxEntries(0))

	const n = 500
	for i := range n {
		mustSet(t, c, "k"+strconv.Itoa(i), nil)
	}
	for i := range n {
		if _, err := c.Get(ctx, "k"+strconv.Itoa(i)); err != nil {
			t.Fatalf("Get(k%d) = %v, want every row kept", i, err)
		}
	}
}

// mustSet writes one row, failing the test on error.
func mustSet(t *testing.T, c kvcache.Cache, key string, expiresAt *time.Time) {
	t.Helper()
	if err := c.Set(context.Background(), kvcache.Entry{
		Key:       key,
		Value:     []byte(key),
		ExpiresAt: expiresAt,
	}); err != nil {
		t.Fatalf("Set(%s): %v", key, err)
	}
}
