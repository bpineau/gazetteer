package httpx

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// pruneFixture returns a cache transport over a fresh dir with `now` pinned,
// plus a writer that plants one entry aged `age` with a body of `size` bytes.
func pruneFixture(t *testing.T, now time.Time) (dir string, plant func(url string, age time.Duration, size int) (metaPath, bodyPath string)) {
	t.Helper()
	dir = t.TempDir()
	r := Options{HTTPCacheDir: dir, RateLimitPerHost: 1000}.resolve()
	r.now = func() time.Time { return now }
	ct := newCacheTransport(unreachableTransport{}, r, dir)

	plant = func(url string, age time.Duration, size int) (string, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		metaPath, bodyPath := ct.pathsFor(requestHash(req))
		body := make([]byte, size)
		for i := range body {
			body[i] = 'x'
		}
		meta := &cacheMeta{
			URL:          url,
			Method:       http.MethodGet,
			Status:       200,
			Header:       http.Header{"Content-Type": []string{"text/plain"}},
			FetchedAtSec: now.Add(-age).Unix(),
			ExpiresAtSec: now.Add(time.Hour).Unix(),
			BodyLen:      int64(size),
		}
		if err := ct.writeEntry(metaPath, bodyPath, meta, body); err != nil {
			t.Fatalf("plant %s: %v", url, err)
		}
		return metaPath, bodyPath
	}
	return dir, plant
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat %s: %v", path, err)
	}
	return err == nil
}

// TestPruneCache_MaxAgeRemovesOldKeepsRecent: the age pass reads the entry's
// fetched_at, not its TTL. A still-fresh entry that nobody has asked for in
// weeks is exactly what a long-lived cache accumulates.
func TestPruneCache_MaxAgeRemovesOldKeepsRecent(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	dir, plant := pruneFixture(t, now)

	oldMeta, oldBody := plant("http://fixture.invalid/old", 48*time.Hour, 100)
	newMeta, newBody := plant("http://fixture.invalid/new", 30*time.Minute, 100)

	stats, err := pruneCacheDir(dir, now, PruneOptions{MaxAge: 24 * time.Hour})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if stats.ScannedEntries != 2 || stats.RemovedEntries != 1 || stats.RemainingEntries != 1 {
		t.Errorf("stats = %+v, want 2 scanned / 1 removed / 1 remaining", stats)
	}
	if stats.RemovedBytes <= 0 || stats.RemainingBytes <= 0 {
		t.Errorf("stats = %+v, want both byte counts populated", stats)
	}
	if exists(t, oldMeta) || exists(t, oldBody) {
		t.Error("the 48h-old entry survived MaxAge=24h")
	}
	if !exists(t, newMeta) || !exists(t, newBody) {
		t.Error("the 30min-old entry was removed by MaxAge=24h")
	}
}

// TestPruneCache_MaxBytesRemovesOldestFirst: the size pass runs after the age
// pass and evicts oldest-fetched-first (the only ordering the on-disk format
// records).
func TestPruneCache_MaxBytesRemovesOldestFirst(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	dir, plant := pruneFixture(t, now)

	oldestMeta, _ := plant("http://fixture.invalid/a", 3*time.Hour, 4000)
	middleMeta, _ := plant("http://fixture.invalid/b", 2*time.Hour, 4000)
	newestMeta, _ := plant("http://fixture.invalid/c", 1*time.Hour, 4000)

	// Room for roughly one entry (each is 4000 body bytes plus a small meta).
	stats, err := pruneCacheDir(dir, now, PruneOptions{MaxBytes: 4500})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if stats.RemovedEntries != 2 || stats.RemainingEntries != 1 {
		t.Errorf("stats = %+v, want 2 removed / 1 remaining", stats)
	}
	if stats.RemainingBytes > 4500 {
		t.Errorf("RemainingBytes = %d, want <= MaxBytes 4500", stats.RemainingBytes)
	}
	if exists(t, oldestMeta) || exists(t, middleMeta) {
		t.Error("the two oldest entries survived the size pass")
	}
	if !exists(t, newestMeta) {
		t.Error("the newest entry was evicted before the older ones")
	}
}

// TestPruneCache_ZeroOptionsOnlyRemovesBrokenEntries: with neither limit set,
// a pass is a no-op on usable entries and still reclaims what the read path
// could never serve (half an entry, an unparseable meta).
func TestPruneCache_ZeroOptionsOnlyRemovesBrokenEntries(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	dir, plant := pruneFixture(t, now)

	goodMeta, goodBody := plant("http://fixture.invalid/good", time.Hour, 10)

	// A body without its meta.
	orphanBody := filepath.Join(dir, "ab", "abcdef.body")
	if err := os.MkdirAll(filepath.Dir(orphanBody), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(orphanBody, []byte("orphan"), 0o644); err != nil {
		t.Fatalf("write orphan body: %v", err)
	}
	// A meta that no longer parses, with its body present.
	brokenMeta, brokenBody := plant("http://fixture.invalid/broken", time.Hour, 10)
	if err := os.WriteFile(brokenMeta, []byte("{not json"), 0o644); err != nil {
		t.Fatalf("corrupt meta: %v", err)
	}

	stats, err := pruneCacheDir(dir, now, PruneOptions{})
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if stats.RemovedEntries != 2 || stats.RemainingEntries != 1 {
		t.Errorf("stats = %+v, want 2 removed (orphan + corrupt) / 1 remaining", stats)
	}
	if exists(t, orphanBody) {
		t.Error("the orphan body survived")
	}
	if exists(t, brokenMeta) || exists(t, brokenBody) {
		t.Error("the corrupt entry survived")
	}
	if !exists(t, goodMeta) || !exists(t, goodBody) {
		t.Error("a usable entry was removed by a zero-valued PruneOptions")
	}
}

// TestPruneCache_ClientSurface covers the exported entry point: the sentinel
// on a cache-less Client, and the empty answer on a directory nothing has
// written yet.
func TestPruneCache_ClientSurface(t *testing.T) {
	noCache := newTestClient(t, Options{RateLimitPerHost: 1000})
	if _, err := noCache.PruneCache(PruneOptions{MaxAge: time.Hour}); !errors.Is(err, ErrCacheDisabled) {
		t.Errorf("PruneCache on a cache-less Client: err = %v, want ErrCacheDisabled", err)
	}

	dir := filepath.Join(t.TempDir(), "never-written")
	c := newTestClient(t, Options{RateLimitPerHost: 1000, HTTPCacheDir: dir})
	stats, err := c.PruneCache(PruneOptions{MaxAge: time.Hour})
	if err != nil {
		t.Fatalf("PruneCache on a missing dir: %v", err)
	}
	if stats != (PruneStats{}) {
		t.Errorf("stats = %+v, want the zero value on a missing dir", stats)
	}
}

// TestPruneCache_KeepsServingAfterAPass is the round trip: an entry that
// survives a pass is still served from disk, and one that was pruned misses.
func TestPruneCache_KeepsServingAfterAPass(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	dir := t.TempDir()
	r := Options{HTTPCacheDir: dir, RateLimitPerHost: 1000}.resolve()
	r.now = func() time.Time { return now }
	ct := newCacheTransport(unreachableTransport{}, r, dir)

	keep, _ := http.NewRequest(http.MethodGet, "http://fixture.invalid/keep", nil)
	drop, _ := http.NewRequest(http.MethodGet, "http://fixture.invalid/drop", nil)
	for _, tc := range []struct {
		req *http.Request
		age time.Duration
	}{{keep, time.Minute}, {drop, 72 * time.Hour}} {
		metaPath, bodyPath := ct.pathsFor(requestHash(tc.req))
		meta := &cacheMeta{
			URL: tc.req.URL.String(), Method: http.MethodGet, Status: 200,
			Header:       http.Header{"Content-Type": []string{"text/plain"}},
			FetchedAtSec: now.Add(-tc.age).Unix(),
			ExpiresAtSec: now.Add(time.Hour).Unix(),
			BodyLen:      5,
		}
		if err := ct.writeEntry(metaPath, bodyPath, meta, []byte("hello")); err != nil {
			t.Fatalf("plant: %v", err)
		}
	}

	if _, err := pruneCacheDir(dir, now, PruneOptions{MaxAge: 24 * time.Hour}); err != nil {
		t.Fatalf("prune: %v", err)
	}

	resp, err := ct.RoundTrip(keep)
	if err != nil {
		t.Fatalf("RoundTrip on the surviving entry: %v", err)
	}
	if resp.Header.Get("X-From-Cache") != "1" {
		t.Error("the surviving entry is no longer served from cache")
	}
	// The pruned entry must miss, i.e. reach the (unreachable) upstream.
	if _, err := ct.RoundTrip(drop); err == nil {
		t.Error("the pruned entry was still served from cache")
	}
}
