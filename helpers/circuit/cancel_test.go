package circuit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/helpers/httpx"
)

// hangingServer answers nothing until the request's context is done.
func hangingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestHTTPFetcher_CallerCancellationDoesNotTripCircuit: the caller cancelling
// (Ctrl-C, an abandoned Collect) aborts every fetch in flight at once. Counting
// those as consecutive transport errors burned the source's breaker for the
// whole process (the flag is never rearmed) over a failure the upstream had
// no part in.
func TestHTTPFetcher_CallerCancellationDoesNotTripCircuit(t *testing.T) {
	srv := hangingServer(t)
	circuit := &atomic.Bool{}
	c, _ := httpx.New(httpx.Options{MaxRetries: -1})
	f := NewHTTPFetcher(c, HTTPFetcherOptions{
		ErrPrefix:                     "demo",
		CircuitTripped:                circuit,
		MaxConsecutiveTransportErrors: 3,
	})

	for i := 1; i <= 5; i++ { // well past the threshold
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			time.Sleep(20 * time.Millisecond)
			cancel()
		}()
		if _, err := f.Fetch(ctx, srv.URL); err == nil {
			cancel()
			t.Fatalf("call %d: a cancelled fetch must still return an error", i)
		}
		cancel()
		if circuit.Load() {
			t.Fatalf("cancellation %d tripped the circuit", i)
		}
	}

	if n := f.consecutiveTransportErrors.Load(); n != 0 {
		t.Errorf("consecutive transport errors = %d after cancellations only, want 0", n)
	}

	// A deadline is the opposite case and must still count: a budget the
	// caller set for this fetch expiring IS the "upstream hung" signal. Fresh
	// client and fetcher, so the host rate limiter the cancelled burst drained
	// does not answer for the upstream.
	c2, _ := httpx.New(httpx.Options{MaxRetries: -1})
	f2 := NewHTTPFetcher(c2, HTTPFetcherOptions{
		ErrPrefix:                     "demo",
		CircuitTripped:                circuit,
		MaxConsecutiveTransportErrors: 3,
	})
	for i := 1; i <= 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, err := f2.Fetch(ctx, srv.URL)
		cancel()
		if err == nil {
			t.Fatalf("deadline call %d: expected an error", i)
		}
	}
	if !circuit.Load() {
		t.Error("3 consecutive deadline failures must still trip the circuit")
	}
}

// TestTransportCircuit_ObserveCtxIgnoresCancellation pins the same rule on the
// standalone breaker, the one sources/dvf folds its outcomes into.
func TestTransportCircuit_ObserveCtxIgnoresCancellation(t *testing.T) {
	flag := &atomic.Bool{}
	tc := NewTransportCircuit("demo-cancel-guard", 2, flag, nil)

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for range 5 {
		tc.ObserveCtx(cancelled, context.Canceled)
	}
	if flag.Load() {
		t.Fatal("caller cancellations tripped the standalone breaker")
	}

	// The same errors, observed under a live context, still count.
	live := context.Background()
	for range 2 {
		tc.ObserveCtx(live, context.DeadlineExceeded)
	}
	if !flag.Load() {
		t.Error("genuine transport failures must still trip the breaker")
	}
}
