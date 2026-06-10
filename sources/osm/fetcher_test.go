package osm

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
