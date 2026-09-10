package cadastre

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// batiResult is what one resolveBatiPolygons call returns, boxed for a channel.
type batiResult struct {
	polygons []BatiPolygon
	cached   bool
	err      error
}

// newBatiServer serves one bâti dump, counting the calls and holding each one
// open until `release` is closed so a sibling caller can join the flight.
// `arrived` fires once, when the first request reaches the handler.
func newBatiServer(t *testing.T, status int, body []byte) (srv *httptest.Server, calls *atomic.Int32, arrived chan struct{}, release chan struct{}) {
	t.Helper()
	calls = &atomic.Int32{}
	arrived = make(chan struct{}, 1)
	release = make(chan struct{})
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case arrived <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
			// A cancellation that leaked from the initiating caller into the
			// shared request would land here; the waiter's assertions then fail.
		}
		w.Header().Set("Content-Type", "application/vnd.geo+json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv, calls, arrived, release
}

// TestResolveBatiPolygons_CoalescesConcurrentSameCommune: a bâti miss is a
// whole-commune dump, so two overlapping resolves of the same INSEE must
// collapse to ONE download and both callers must get the polygons.
func TestResolveBatiPolygons_CoalescesConcurrentSameCommune(t *testing.T) {
	srv, calls, arrived, release := newBatiServer(t, http.StatusOK, buildSyntheticBatiAroundSmallCommune(t))
	s := NewSource(Options{BatiBaseURL: srv.URL, IncludeBati: true})

	out := make(chan batiResult, 2)
	resolve := func(ctx context.Context) {
		polys, _, cached, _, err := s.resolveBatiPolygons(ctx, "27078")
		out <- batiResult{polys, cached, err}
	}

	go resolve(context.Background())
	<-arrived                         // the first caller owns the in-flight download
	go resolve(context.Background())  // joins it (same INSEE)
	time.Sleep(40 * time.Millisecond) // let the second caller attach
	close(release)

	for range 2 {
		res := <-out
		if res.err != nil {
			t.Fatalf("resolveBatiPolygons: %v", res.err)
		}
		if len(res.polygons) == 0 {
			t.Fatal("coalesced caller got no polygons")
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("upstream calls = %d, want 1 (concurrent same-commune misses must coalesce)", got)
	}
	// The flight released its key, so the next call is served by the cache.
	polys, _, cached, _, err := s.resolveBatiPolygons(context.Background(), "27078")
	if err != nil || !cached || len(polys) == 0 {
		t.Errorf("post-flight resolve = (%d polygons, cached=%v, err=%v), want a cache hit", len(polys), cached, err)
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("upstream calls = %d after the cache hit, want 1", got)
	}
}

// TestResolveBatiPolygons_ErrorReachesEveryWaiter: a failed dump must surface
// AS an error to every coalesced caller (each then soft-fails per the bâti
// contract), never be shared as an empty success and never be cached.
func TestResolveBatiPolygons_ErrorReachesEveryWaiter(t *testing.T) {
	srv, calls, arrived, release := newBatiServer(t, http.StatusInternalServerError, []byte("upstream down"))
	s := NewSource(Options{BatiBaseURL: srv.URL, IncludeBati: true})

	out := make(chan batiResult, 2)
	resolve := func() {
		polys, _, cached, _, err := s.resolveBatiPolygons(context.Background(), "27078")
		out <- batiResult{polys, cached, err}
	}
	go resolve()
	<-arrived
	go resolve()
	time.Sleep(40 * time.Millisecond)
	close(release)

	for range 2 {
		res := <-out
		if res.err == nil {
			t.Fatalf("coalesced caller got err = nil, want the shared failure (polygons=%d)", len(res.polygons))
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("upstream calls = %d, want 1", got)
	}
	if _, ok := s.defaultCache.Get("27078"); ok {
		t.Error("a failed dump must not be cached")
	}
}

// TestResolveBatiPolygons_CancelledInitiatorDoesNotFailWaiter: the caller that
// happens to start the flight must not be able to cancel it out from under the
// callers that joined it. It returns its own ctx error promptly; they still get
// the dump.
func TestResolveBatiPolygons_CancelledInitiatorDoesNotFailWaiter(t *testing.T) {
	srv, calls, arrived, release := newBatiServer(t, http.StatusOK, buildSyntheticBatiAroundSmallCommune(t))
	s := NewSource(Options{BatiBaseURL: srv.URL, IncludeBati: true})

	initiatorCtx, cancelInitiator := context.WithCancel(context.Background())
	initiatorErr := make(chan error, 1)
	go func() {
		_, _, _, _, err := s.resolveBatiPolygons(initiatorCtx, "27078")
		initiatorErr <- err
	}()
	<-arrived

	waiter := make(chan batiResult, 1)
	go func() {
		polys, _, cached, _, err := s.resolveBatiPolygons(context.Background(), "27078")
		waiter <- batiResult{polys, cached, err}
	}()
	time.Sleep(40 * time.Millisecond) // let the waiter attach to the flight

	cancelInitiator()
	select {
	case err := <-initiatorErr:
		if err == nil {
			t.Fatal("cancelled initiator returned nil error, want its context error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled initiator did not return promptly")
	}

	close(release)
	select {
	case res := <-waiter:
		if res.err != nil {
			t.Fatalf("surviving waiter failed after the initiator cancelled: %v", res.err)
		}
		if len(res.polygons) == 0 {
			t.Fatal("surviving waiter got no polygons (the cancellation poisoned the shared flight)")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("surviving waiter never completed")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("upstream calls = %d, want 1", got)
	}
}
