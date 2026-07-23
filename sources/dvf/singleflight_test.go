package dvf

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/helpers/httpx"
)

// sfHTTPClient builds an httpx client with throttling effectively off and
// no retry tax, so the concurrency of these tests is not serialised by the
// token bucket and error paths surface immediately.
func sfHTTPClient(t *testing.T) *httpx.Client {
	t.Helper()
	c, err := httpx.New(httpx.Options{
		RateLimitPerHost:  1000,
		MaxRetries:        1,
		BaseRetryInterval: 5 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

type mutResult struct {
	resp MutationsResponse
	err  error
}

// TestFetchMutations_CoalescesConcurrentSameKey verifies that two
// overlapping fetches for the SAME (insee, section) collapse to ONE
// upstream call — the cross-request single-flight guarantee.
func TestFetchMutations_CoalescesConcurrentSameKey(t *testing.T) {
	body := loadFixtureMutations(t, "dvfapi_mutations_75107_AD.json")

	var calls atomic.Int32
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		select {
		case arrived <- struct{}{}:
		default:
		}
		<-release // hold the flight open so a sibling can join it
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	withBaseURL(t, srv.URL+"/mutations")

	s := NewSource(Options{HTTP: sfHTTPClient(t)})

	out := make(chan mutResult, 2)
	fetch := func() {
		r, err := s.fetchMutations(context.Background(), "75107", "000AD")
		out <- mutResult{r, err}
	}

	go fetch()
	<-arrived  // the first flight is now in-flight, blocked in the handler
	go fetch() // joins the in-flight flight (same key)
	// The first caller cannot proceed until we close(release), so the key
	// stays in flight; this window lets the second caller attach to it.
	time.Sleep(40 * time.Millisecond)
	close(release)

	for range 2 {
		res := <-out
		if res.err != nil {
			t.Fatalf("fetchMutations: %v", res.err)
		}
		if len(res.resp.Data) == 0 {
			t.Fatalf("expected mutations, got 0")
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("upstream calls = %d, want 1 (concurrent same-key fetches must coalesce)", got)
	}
}

// TestFetchMutations_DifferentSectionsProceedInParallel verifies that
// distinct (insee, section) keys are NOT coalesced: both reach the
// upstream and run concurrently. The handler barrier (both requests must
// arrive before either responds) would deadlock if they were serialised or
// merged.
func TestFetchMutations_DifferentSectionsProceedInParallel(t *testing.T) {
	body := loadFixtureMutations(t, "dvfapi_mutations_75107_AD.json")

	var calls atomic.Int32
	var barrier sync.WaitGroup
	barrier.Add(2)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		barrier.Done()
		barrier.Wait() // proceed only once BOTH distinct requests have arrived
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	withBaseURL(t, srv.URL+"/mutations")

	s := NewSource(Options{HTTP: sfHTTPClient(t)})

	done := make(chan error, 2)
	for _, sec := range []string{"000AA", "000AB"} {
		go func(sec string) {
			_, err := s.fetchMutations(context.Background(), "75107", sec)
			done <- err
		}(sec)
	}

	timeout := time.After(5 * time.Second)
	for range 2 {
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("fetchMutations: %v", err)
			}
		case <-timeout:
			t.Fatal("distinct sections did not run in parallel (barrier deadlocked)")
		}
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("upstream calls = %d, want 2 (distinct sections must not coalesce)", got)
	}
}

// TestFetchMutations_CancelledInitiatorDoesNotFailWaiter verifies that an
// initiator whose ctx is cancelled mid-flight returns promptly with its own
// ctx error WITHOUT cancelling the shared upstream request — the surviving
// waiter still receives the successful result.
func TestFetchMutations_CancelledInitiatorDoesNotFailWaiter(t *testing.T) {
	body := loadFixtureMutations(t, "dvfapi_mutations_75107_AD.json")

	var calls atomic.Int32
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case arrived <- struct{}{}:
		default:
		}
		select {
		case <-release:
		case <-r.Context().Done():
			// If the initiator's cancellation leaked into the shared
			// request, we'd land here — the test then fails on the empty
			// waiter result below.
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	withBaseURL(t, srv.URL+"/mutations")

	s := NewSource(Options{HTTP: sfHTTPClient(t)})

	initiatorCtx, cancelInitiator := context.WithCancel(context.Background())
	initiatorErr := make(chan error, 1)
	go func() {
		_, err := s.fetchMutations(initiatorCtx, "75107", "000AD")
		initiatorErr <- err
	}()
	<-arrived // the initiator owns the in-flight request

	waiterOut := make(chan mutResult, 1)
	go func() {
		r, err := s.fetchMutations(context.Background(), "75107", "000AD")
		waiterOut <- mutResult{r, err}
	}()
	time.Sleep(40 * time.Millisecond) // let the waiter attach to the flight

	cancelInitiator() // initiator gives up mid-flight
	select {
	case err := <-initiatorErr:
		if err == nil {
			t.Fatal("cancelled initiator returned nil error, want context cancellation")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled initiator did not return promptly")
	}

	close(release) // shared request completes

	select {
	case res := <-waiterOut:
		if res.err != nil {
			t.Fatalf("surviving waiter failed after initiator cancel: %v", res.err)
		}
		if len(res.resp.Data) == 0 {
			t.Fatal("surviving waiter got no data (initiator cancel poisoned the shared flight)")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("surviving waiter never completed")
	}

	if got := calls.Load(); got != 1 {
		t.Errorf("upstream calls = %d, want 1", got)
	}
}

// TestFetchMutations_ErrorPropagatesToAllWaiters verifies that a failed
// shared fetch surfaces as an error to every coalesced caller (never shared
// as a successful empty response — the breaker-tripped path relies on this).
func TestFetchMutations_ErrorPropagatesToAllWaiters(t *testing.T) {
	arrived := make(chan struct{}, 1)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case arrived <- struct{}{}:
		default:
		}
		<-release
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	withBaseURL(t, srv.URL+"/mutations")

	s := NewSource(Options{HTTP: sfHTTPClient(t)})

	out := make(chan mutResult, 2)
	fetch := func() {
		r, err := s.fetchMutations(context.Background(), "75107", "000AD")
		out <- mutResult{r, err}
	}
	go fetch()
	<-arrived
	go fetch()
	time.Sleep(40 * time.Millisecond)
	close(release)

	for range 2 {
		res := <-out
		if res.err == nil {
			t.Errorf("expected error to propagate to waiter, got nil (len(Data)=%d)", len(res.resp.Data))
		}
	}
}
