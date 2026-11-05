package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// echoServer answers with the request's Authorization (or Accept-Language)
// value and the given Cache-Control and Vary, counting its hits.
func echoServer(t *testing.T, cacheControl, vary string, hits *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		if cacheControl != "" {
			w.Header().Set("Cache-Control", cacheControl)
		}
		if vary != "" {
			w.Header().Set("Vary", vary)
		}
		_, _ = w.Write([]byte(r.Header.Get("Authorization") + "|" + r.Header.Get("Accept-Language")))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func get(t *testing.T, c *Client, u string, hdr http.Header) string {
	t.Helper()
	b, _, err := c.GetBytes(context.Background(), u, hdr)
	if err != nil {
		t.Fatalf("GET %s: %v", u, err)
	}
	return string(b)
}

// TestCache_CredentialsAreNeverShared pins that two identities never see
// each other's response, and that nothing they fetch lands on disk.
func TestCache_CredentialsAreNeverShared(t *testing.T) {
	var hits int32
	srv := echoServer(t, "max-age=600", "Authorization", &hits)
	dir := t.TempDir()
	c := newTestClient(t, Options{HTTPCacheDir: dir, RateLimitPerHost: 1000})

	if got := get(t, c, srv.URL, http.Header{"Authorization": {"A"}}); !strings.HasPrefix(got, "A|") {
		t.Fatalf("first identity read %q", got)
	}
	if got := get(t, c, srv.URL, http.Header{"Authorization": {"B"}}); !strings.HasPrefix(got, "B|") {
		t.Errorf("second identity read %q, want its own response", got)
	}
	if hits != 2 {
		t.Errorf("upstream hits = %d, want 2", hits)
	}
	if n := countBodies(t, dir); n != 0 {
		t.Errorf("%d cache bodies written for credentialed requests, want 0", n)
	}

	// URL userinfo is a credential too.
	u, _ := url.Parse(srv.URL)
	u.User = url.UserPassword("user", "secret")
	_ = get(t, c, u.String(), nil)
	if n := countBodies(t, dir); n != 0 {
		t.Errorf("%d cache bodies written for a userinfo URL, want 0", n)
	}
}

// TestCache_RequestHeadersPartitionEntries pins Vary-correctness: a response
// negotiated on a request header is not served to a request that differs on
// it, and a "Vary: *" response is not stored.
func TestCache_RequestHeadersPartitionEntries(t *testing.T) {
	var hits int32
	srv := echoServer(t, "max-age=600", "Accept-Language", &hits)
	c := newTestClient(t, Options{HTTPCacheDir: t.TempDir(), RateLimitPerHost: 1000})

	fr := http.Header{"Accept-Language": {"fr"}}
	en := http.Header{"Accept-Language": {"en"}}
	if get(t, c, srv.URL, fr) != "|fr" || get(t, c, srv.URL, en) != "|en" || get(t, c, srv.URL, fr) != "|fr" {
		t.Fatal("a language variant was served to the other language")
	}
	if hits != 2 {
		t.Errorf("upstream hits = %d, want 2 (one per variant, the repeat from cache)", hits)
	}

	var starHits int32
	star := echoServer(t, "max-age=600", "*", &starHits)
	dir := t.TempDir()
	c = newTestClient(t, Options{HTTPCacheDir: dir, RateLimitPerHost: 1000})
	_ = get(t, c, star.URL, nil)
	_ = get(t, c, star.URL, nil)
	if starHits != 2 || countBodies(t, dir) != 0 {
		t.Errorf("Vary: * gave %d hits and %d bodies, want 2 and 0", starHits, countBodies(t, dir))
	}
}

// TestCache_DirectiveOrderDoesNotMatter pins that no-store and no-cache win
// over max-age whatever their position in Cache-Control.
func TestCache_DirectiveOrderDoesNotMatter(t *testing.T) {
	for _, cc := range []string{"no-store", "max-age=600, no-store", "no-store, max-age=600"} {
		var hits int32
		srv := echoServer(t, cc, "", &hits)
		dir := t.TempDir()
		c := newTestClient(t, Options{HTTPCacheDir: dir, RateLimitPerHost: 1000})
		_ = get(t, c, srv.URL, nil)
		_ = get(t, c, srv.URL, nil)
		// A new Client over the same dir must not find anything either.
		_ = get(t, newTestClient(t, Options{HTTPCacheDir: dir, RateLimitPerHost: 1000}), srv.URL, nil)
		if hits != 3 || countBodies(t, dir) != 0 {
			t.Errorf("%q: %d hits, %d bodies; want 3 and 0", cc, hits, countBodies(t, dir))
		}
	}
	for _, cc := range []string{"max-age=600, no-cache", "no-cache, max-age=600"} {
		var hits int32
		srv := echoServer(t, cc, "", &hits)
		c := newTestClient(t, Options{HTTPCacheDir: t.TempDir(), RateLimitPerHost: 1000})
		_ = get(t, c, srv.URL, nil)
		_ = get(t, c, srv.URL, nil)
		if hits != 2 {
			t.Errorf("%q: %d hits, want 2 (no-cache revalidates every time)", cc, hits)
		}
	}
}

// TestCache_HostDefaultTTL pins that HostOptions.DefaultTTL overrides the
// client-wide fallback TTL for its host only.
func TestCache_HostDefaultTTL(t *testing.T) {
	var hits int32
	srv := echoServer(t, "", "", &hits)
	host := srv.Listener.Addr().String()
	now := time.Unix(1_700_000_000, 0)
	short := time.Minute
	c := newTestClient(t, Options{
		HTTPCacheDir:     t.TempDir(),
		RateLimitPerHost: 1000,
		DefaultTTL:       24 * time.Hour,
		PerHost:          map[string]HostOptions{host: {DefaultTTL: &short}},
		Now:              func() time.Time { return now },
	})
	_ = get(t, c, srv.URL, nil)
	now = now.Add(2 * time.Minute) // past the host TTL, well within the global one
	_ = get(t, c, srv.URL, nil)
	if hits != 2 {
		t.Errorf("upstream hits = %d, want 2: the 1-minute host TTL expired the entry", hits)
	}
}

// countBodies counts the cache body files under dir.
func countBodies(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(p) == ".body" {
			n++
		}
		return nil
	})
	return n
}
