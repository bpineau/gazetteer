package osm

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/helpers/httpx"
)

// A hung primary must not starve the fallback: each mirror gets its own
// time slice, so the healthy mirror still answers within the caller's
// budget (the old shape shared one deadline across the walk and the
// fallback always saw an expired context).
func TestQuery_HungPrimaryFallbackRescues(t *testing.T) {
	t.Parallel()
	hung := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select { // hang well past the per-attempt slice
		case <-r.Context().Done():
		case <-time.After(1500 * time.Millisecond):
		}
	}))
	defer hung.Close()
	healthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"elements":[]}`))
	}))
	defer healthy.Close()

	hc, err := httpx.New(httpx.Options{})
	if err != nil {
		t.Fatalf("httpx: %v", err)
	}
	f := NewHTTPOverpassFetcher(hc, hung.URL)
	f.fallbacks = []string{healthy.URL}
	f.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))

	// Shrink the per-attempt slice for test speed.
	restore := setMirrorTimeoutForTest(t, 300*time.Millisecond)
	defer restore()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	body, err := f.Query(ctx, "[out:json];node(1);out;")
	if err != nil {
		t.Fatalf("Query: %v (fallback should have rescued)", err)
	}
	if string(body) != `{"elements":[]}` {
		t.Errorf("body = %s", body)
	}

	// Streak skip: after mirrorSkipThreshold failures the hung mirror is
	// skipped outright — Query goes straight to the healthy fallback.
	for range mirrorSkipThreshold { // already has 1 failure; overshoot is fine
		_, _ = f.Query(ctx, "[out:json];node(1);out;")
	}
	start := time.Now()
	if _, err := f.Query(ctx, "[out:json];node(1);out;"); err != nil {
		t.Fatalf("Query after skip: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 200*time.Millisecond {
		t.Errorf("skipped-primary query took %v, want fast path (no hung-mirror tax)", elapsed)
	}
}

// setMirrorTimeoutForTest shrinks the per-mirror slice and returns a restore func.
func setMirrorTimeoutForTest(t *testing.T, d time.Duration) func() {
	t.Helper()
	old := overpassMirrorTimeout
	overpassMirrorTimeout = d
	return func() { overpassMirrorTimeout = old }
}

// SetLogger races with Query: the live Source keeps a long-lived fetcher and
// the refresher swaps its logger, so the logger field must be published
// safely. Run under -race, this test reports a data race whenever the field
// is a plain, unsynchronized *slog.Logger.
func TestSetLogger_ConcurrentWithQuery(t *testing.T) {
	// Deliberately NOT t.Parallel(): Query reads the package-level
	// overpassMirrorTimeout, which the parallel fallback test rewrites
	// through setMirrorTimeoutForTest. Staying sequential keeps this test
	// (and -race) about the logger field, not about that test seam.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"elements":[]}`))
	}))
	defer srv.Close()

	// A high per-host rate: the point is the race, not the politeness
	// (HTTPClient().Do still goes through httpx's limiting transport).
	hc, err := httpx.New(httpx.Options{RateLimitPerHost: 1000, BurstPerHost: 1000})
	if err != nil {
		t.Fatalf("httpx: %v", err)
	}
	f := NewHTTPOverpassFetcher(hc, srv.URL)
	quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
	f.SetLogger(quiet)

	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for range 50 {
			if _, err := f.Query(ctx, "[out:json];node(1);out;"); err != nil {
				t.Errorf("Query: %v", err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 50 {
			f.SetLogger(slog.New(slog.NewTextHandler(io.Discard, nil)))
		}
	}()
	wg.Wait()
}
