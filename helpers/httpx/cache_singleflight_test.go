package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// getResult is one GetBytes outcome, boxed for a channel.
type getResult struct {
	body   []byte
	resp   *Response
	status int
	err    error
}

// blockingServer counts requests and holds each one open until `release` is
// closed, so a sibling caller can join the flight the first one started.
// `arrived` fires once, when the first request reaches the handler.
func blockingServer(t *testing.T, handle func(w http.ResponseWriter)) (srv *httptest.Server, calls *atomic.Int32, arrived chan struct{}, release chan struct{}) {
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
			// A cancellation leaking from the initiating caller into the
			// shared request would land here; the waiter's assertions fail.
		}
		handle(w)
	}))
	t.Cleanup(srv.Close)
	return srv, calls, arrived, release
}

// TestCacheMiss_CoalescesConcurrentSameKey: a cold entry hit by two identical
// requests at once must cost ONE upstream call, with both callers getting the
// body (and the entry written once).
func TestCacheMiss_CoalescesConcurrentSameKey(t *testing.T) {
	srv, calls, arrived, release := blockingServer(t, func(w http.ResponseWriter) {
		w.Header().Set("Cache-Control", "max-age=3600")
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("hello"))
	})
	c := newTestClient(t, Options{HTTPCacheDir: t.TempDir(), RateLimitPerHost: 1000})

	out := make(chan getResult, 2)
	get := func(ctx context.Context) {
		body, resp, err := c.GetBytes(ctx, srv.URL+"/x", nil)
		out <- getResult{body: body, resp: resp, err: err}
	}

	go get(context.Background())
	<-arrived
	go get(context.Background())
	time.Sleep(40 * time.Millisecond) // let the second caller attach
	close(release)

	for range 2 {
		res := <-out
		if res.err != nil {
			t.Fatalf("GetBytes: %v", res.err)
		}
		if string(res.body) != "hello" {
			t.Fatalf("body = %q, want %q", res.body, "hello")
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("upstream calls = %d, want 1 (concurrent misses of one key must coalesce)", got)
	}

	// The flight released its key; the entry it wrote now serves from disk.
	_, resp, err := c.GetBytes(context.Background(), srv.URL+"/x", nil)
	if err != nil {
		t.Fatalf("third GetBytes: %v", err)
	}
	if !resp.FromCache {
		t.Error("the entry the flight wrote is not being served from cache")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("upstream calls = %d after the cache hit, want 1", got)
	}
}

// TestCacheMiss_ErrorReachesEveryWaiterAndIsNotCached: a non-cacheable answer
// (here a 500) must reach every coalesced caller as the error it is, and must
// NOT be banked: the next request goes back to the upstream.
func TestCacheMiss_ErrorReachesEveryWaiterAndIsNotCached(t *testing.T) {
	srv, calls, arrived, release := blockingServer(t, func(w http.ResponseWriter) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	c := newTestClient(t, Options{
		HTTPCacheDir:     t.TempDir(),
		RateLimitPerHost: 1000,
		MaxRetries:       -1, // one attempt: the 500 must surface immediately
	})

	out := make(chan getResult, 2)
	get := func() {
		body, resp, err := c.GetBytes(context.Background(), srv.URL+"/x", nil)
		status := 0
		if resp != nil {
			status = resp.Status
		}
		out <- getResult{body: body, status: status, err: err}
	}
	go get()
	<-arrived
	go get()
	time.Sleep(40 * time.Millisecond)
	close(release)

	for range 2 {
		res := <-out
		herr, ok := errors.AsType[*ErrHTTP](res.err)
		if !ok {
			t.Fatalf("err = %v (%T), want *ErrHTTP for both coalesced callers", res.err, res.err)
		}
		if herr.Status != http.StatusInternalServerError || res.status != http.StatusInternalServerError {
			t.Errorf("status = %d / %d, want 500 for both", herr.Status, res.status)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("upstream calls = %d, want 1", got)
	}

	// Nothing was cached: the next request must reach the upstream again.
	if _, _, err := c.GetBytes(context.Background(), srv.URL+"/x", nil); err == nil {
		t.Error("the 500 was served from cache, want a fresh upstream trip")
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("upstream calls = %d after the retry, want 2 (a failure must not be cached)", got)
	}
}

// TestCacheMiss_CancelledInitiatorDoesNotFailWaiter: the caller that starts a
// flight must not cancel it for the callers that joined it. It returns its own
// context error promptly; they still get the body.
func TestCacheMiss_CancelledInitiatorDoesNotFailWaiter(t *testing.T) {
	srv, calls, arrived, release := blockingServer(t, func(w http.ResponseWriter) {
		w.Header().Set("Cache-Control", "max-age=3600")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("hello"))
	})
	c := newTestClient(t, Options{HTTPCacheDir: t.TempDir(), RateLimitPerHost: 1000})

	initiatorCtx, cancelInitiator := context.WithCancel(context.Background())
	initiatorErr := make(chan error, 1)
	go func() {
		_, _, err := c.GetBytes(initiatorCtx, srv.URL+"/x", nil)
		initiatorErr <- err
	}()
	<-arrived

	waiter := make(chan getResult, 1)
	go func() {
		body, resp, err := c.GetBytes(context.Background(), srv.URL+"/x", nil)
		waiter <- getResult{body: body, resp: resp, err: err}
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
		if string(res.body) != "hello" {
			t.Fatalf("surviving waiter body = %q, want %q", res.body, "hello")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("surviving waiter never completed")
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("upstream calls = %d, want 1", got)
	}
}

// TestCacheMiss_DistinctKeysRunInParallel: only identical keys coalesce. The
// handler barrier (both requests must arrive before either answers) would
// deadlock if two different URLs were merged or serialised.
func TestCacheMiss_DistinctKeysRunInParallel(t *testing.T) {
	var calls atomic.Int32
	both := make(chan struct{}, 2)
	proceed := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		both <- struct{}{}
		<-proceed
		w.Header().Set("Cache-Control", "max-age=3600")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("hello"))
	}))
	defer srv.Close()
	c := newTestClient(t, Options{HTTPCacheDir: t.TempDir(), RateLimitPerHost: 1000})

	done := make(chan error, 2)
	for _, path := range []string{"/a", "/b"} {
		go func(path string) {
			_, _, err := c.GetBytes(context.Background(), srv.URL+path, nil)
			done <- err
		}(path)
	}

	for range 2 {
		select {
		case <-both:
		case <-time.After(2 * time.Second):
			t.Fatal("distinct keys did not reach the upstream in parallel")
		}
	}
	close(proceed)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatalf("GetBytes: %v", err)
		}
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("upstream calls = %d, want 2 (distinct keys must not coalesce)", got)
	}
}
