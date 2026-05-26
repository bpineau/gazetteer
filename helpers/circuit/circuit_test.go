package circuit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/http2"

	"github.com/bpineau/gazetteer/helpers/httpx"
)

func TestFuncFetcher_DelegatesToFunction(t *testing.T) {
	want := []byte("hello")
	f := FuncFetcher(func(_ context.Context, url string) ([]byte, error) {
		if url != "https://x" {
			t.Errorf("url = %q want %q", url, "https://x")
		}
		return want, nil
	})
	got, err := f.Fetch(context.Background(), "https://x")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("body = %q want %q", got, want)
	}
}

func TestHTTPFetcher_NilGuard(t *testing.T) {
	var h *HTTPFetcher
	_, err := h.Fetch(context.Background(), "https://x")
	if err == nil || !strings.Contains(err.Error(), "nil HTTPFetcher") {
		t.Fatalf("expected nil-guard error, got %v", err)
	}
}

func TestHTTPFetcher_NilClientGuard(t *testing.T) {
	h := NewHTTPFetcher(nil, HTTPFetcherOptions{ErrPrefix: "demo"})
	_, err := h.Fetch(context.Background(), "https://x")
	if err == nil || !strings.Contains(err.Error(), "demo: nil HTTPFetcher") {
		t.Fatalf("expected 'demo: nil HTTPFetcher', got %v", err)
	}
}

func TestHTTPFetcher_AppliesHeaders(t *testing.T) {
	var (
		gotAccept string
		gotXrw    string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotXrw = r.Header.Get("X-Requested-With")
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c, _ := httpx.New(httpx.Options{})
	hdr := http.Header{}
	hdr.Set("Accept", "application/json")
	hdr.Set("X-Requested-With", "XMLHttpRequest")
	f := NewHTTPFetcher(c, HTTPFetcherOptions{ErrPrefix: "demo", Headers: hdr})

	body, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q want ok", body)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q want application/json", gotAccept)
	}
	if gotXrw != "XMLHttpRequest" {
		t.Errorf("X-Requested-With = %q want XMLHttpRequest", gotXrw)
	}
}

func TestHTTPFetcher_WrapsTransportErr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(503)
		_, _ = w.Write([]byte("oops"))
	}))
	defer srv.Close()

	// MaxRetries: -1 disables the retry loop so a 503 fixture returns on
	// the first attempt. The default would retry 5 times with exponential
	// backoff (500 ms × 2^n) and wedge this test at ~15 s.
	c, _ := httpx.New(httpx.Options{MaxRetries: -1})
	f := NewHTTPFetcher(c, HTTPFetcherOptions{ErrPrefix: "demo"})
	_, err := f.Fetch(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error on 503")
	}
	if !strings.Contains(err.Error(), "demo fetch") {
		t.Errorf("err = %v: missing 'demo fetch' prefix", err)
	}
	// Sentinel ErrHTTP must remain reachable through %w.
	var herr *httpx.ErrHTTP
	if !errors.As(err, &herr) {
		t.Errorf("err = %v: expected to wrap *httpx.ErrHTTP", err)
	}
}

func TestNewHTTPFetcher_DefaultsErrPrefix(t *testing.T) {
	c, _ := httpx.New(httpx.Options{})
	f := NewHTTPFetcher(c, HTTPFetcherOptions{})
	if f.Options.ErrPrefix != "common" {
		t.Errorf("default ErrPrefix = %q want common", f.Options.ErrPrefix)
	}
}

// HTTP 429 with no retry budget left must trip the shared atomic, and
// a successful response carrying `x-quota-remaining: 0` must too. Both
// signals tell the caller "skip further fetches for the rest of this
// run".
// Once the shared atomic is flipped, HTTPFetcher.Fetch MUST refuse to
// issue any further HTTP request without touching the upstream.
// Regression for the live retry-storm where every Fetch after the
// trip paid a 5×exp-backoff retry tax before surfacing the same 429
// to the caller. With the pre-flight check in place, every subsequent
// Fetch returns ErrCircuitOpen in O(1).
func TestHTTPFetcher_RefusesWhenCircuitAlreadyOpen(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	flag := &atomic.Bool{}
	flag.Store(true) // pre-trip
	c, _ := httpx.New(httpx.Options{})
	f := NewHTTPFetcher(c, HTTPFetcherOptions{ErrPrefix: "demo", CircuitTripped: flag})
	for i := 0; i < 5; i++ {
		_, err := f.Fetch(context.Background(), srv.URL)
		if !errors.Is(err, ErrCircuitOpen) {
			t.Fatalf("call %d: want ErrCircuitOpen, got %v", i, err)
		}
	}
	if got := hits.Load(); got != 0 {
		t.Errorf("upstream received %d requests while circuit was open, want 0", got)
	}
}

func TestHTTPFetcher_QuotaTripped_On429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
	}))
	defer srv.Close()

	quotaTripped := &atomic.Bool{}
	c, _ := httpx.New(httpx.Options{MaxRetries: -1})
	f := NewHTTPFetcher(c, HTTPFetcherOptions{ErrPrefix: "demo", QuotaTripped: quotaTripped})
	_, err := f.Fetch(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error on 429")
	}
	if !quotaTripped.Load() {
		t.Error("expected QuotaTripped to be true after 429")
	}
}

func TestHTTPFetcher_QuotaTripped_OnHeaderZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-quota-remaining", "0")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	quotaTripped := &atomic.Bool{}
	c, _ := httpx.New(httpx.Options{})
	f := NewHTTPFetcher(c, HTTPFetcherOptions{ErrPrefix: "demo", QuotaTripped: quotaTripped})
	_, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !quotaTripped.Load() {
		t.Error("expected QuotaTripped to be true after x-quota-remaining: 0")
	}
}

func TestHTTPFetcher_QuotaTripped_NotFlippedOnHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("x-quota-remaining", "9999")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()

	quotaTripped := &atomic.Bool{}
	c, _ := httpx.New(httpx.Options{})
	f := NewHTTPFetcher(c, HTTPFetcherOptions{ErrPrefix: "demo", QuotaTripped: quotaTripped})
	_, err := f.Fetch(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if quotaTripped.Load() {
		t.Error("expected QuotaTripped to stay false on healthy response")
	}
}

// N consecutive context-deadline failures must trip the circuit. We
// simulate a hanging upstream and a tight per-request context — each
// Fetch returns context.DeadlineExceeded (wrapped in *ErrTransport via
// httpx). Threshold = 3, so call 3 must flip the atomic.
func TestHTTPFetcher_CircuitTripped_AfterNConsecutiveTransportErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Hang past the per-request context budget.
		<-r.Context().Done()
		w.WriteHeader(504)
	}))
	defer srv.Close()

	circuit := &atomic.Bool{}
	c, _ := httpx.New(httpx.Options{MaxRetries: -1})
	f := NewHTTPFetcher(c, HTTPFetcherOptions{
		ErrPrefix:                     "demo",
		CircuitTripped:                circuit,
		MaxConsecutiveTransportErrors: 3,
	})

	for i := 1; i <= 3; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, err := f.Fetch(ctx, srv.URL)
		cancel()
		if err == nil {
			t.Fatalf("call %d: expected error", i)
		}
		if i < 3 && circuit.Load() {
			t.Errorf("circuit tripped early on call %d", i)
		}
	}
	if !circuit.Load() {
		t.Error("expected CircuitTripped to be true after 3 consecutive transport errors")
	}
}

// A 2xx between failures must reset the counter — 2 fail + 1 success +
// 2 fail = no trip (we never reach 3 in a row).
func TestHTTPFetcher_CircuitTripped_ResetsOnSuccess(t *testing.T) {
	var (
		mu         sync.Mutex
		mode       string // "fail" or "ok"
		patternIdx int
	)
	pattern := []string{"fail", "fail", "ok", "fail", "fail"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		if patternIdx < len(pattern) {
			mode = pattern[patternIdx]
			patternIdx++
		}
		localMode := mode
		mu.Unlock()
		if localMode == "fail" {
			<-r.Context().Done()
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	circuit := &atomic.Bool{}
	c, _ := httpx.New(httpx.Options{MaxRetries: -1})
	f := NewHTTPFetcher(c, HTTPFetcherOptions{
		ErrPrefix:                     "demo",
		CircuitTripped:                circuit,
		MaxConsecutiveTransportErrors: 3,
	})

	for range pattern {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
		_, _ = f.Fetch(ctx, srv.URL)
		cancel()
	}
	if circuit.Load() {
		t.Error("circuit must NOT trip when a 2xx breaks the run (counter resets)")
	}
}

// MaxConsecutiveTransportErrors=0 must keep the consecutive-error
// breaker disabled (quota signals still trip the flag, but a raw
// transport error must not).
func TestHTTPFetcher_CircuitTripped_DisabledWhenThresholdZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		w.WriteHeader(504)
	}))
	defer srv.Close()

	circuit := &atomic.Bool{}
	c, _ := httpx.New(httpx.Options{MaxRetries: -1})
	f := NewHTTPFetcher(c, HTTPFetcherOptions{
		ErrPrefix:                     "demo",
		CircuitTripped:                circuit,
		MaxConsecutiveTransportErrors: 0,
	})

	for range 5 {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, _ = f.Fetch(ctx, srv.URL)
		cancel()
	}
	if circuit.Load() {
		t.Error("circuit must NOT trip when MaxConsecutiveTransportErrors=0")
	}
}

// The process-wide trip counter must increment exactly once per
// false→true flip of the shared atomic — not on every subsequent Fetch
// that observes the already-tripped flag. The metrics handler exposes
// this counter as `encheridor_enrich_circuit_tripped_total{source}` so
// it must reflect "number of flips" semantics.
func TestHTTPFetcher_CircuitTripCounter_BumpsOnce(t *testing.T) {
	ResetCircuitTripCountersForTest()
	t.Cleanup(ResetCircuitTripCountersForTest)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
	}))
	defer srv.Close()

	quotaTripped := &atomic.Bool{}
	c, _ := httpx.New(httpx.Options{MaxRetries: -1})
	f := NewHTTPFetcher(c, HTTPFetcherOptions{ErrPrefix: "demosrc", QuotaTripped: quotaTripped})

	// Three 429s in a row. The first one flips the atomic ; the next
	// two observe it already tripped and must NOT re-bump the counter.
	for range 3 {
		_, _ = f.Fetch(context.Background(), srv.URL)
	}
	snap := SnapshotCircuitTripCounts()
	if len(snap) != 1 {
		t.Fatalf("snapshot len = %d, want 1: %+v", len(snap), snap)
	}
	if snap[0].Source != "demosrc" {
		t.Errorf("source = %q, want demosrc", snap[0].Source)
	}
	if snap[0].Count != 1 {
		t.Errorf("count = %d, want 1 (one flip, not three)", snap[0].Count)
	}
}

// The live-state snapshot reads each registered source's *atomic.Bool
// at call time, so flipping the flag changes the next snapshot reading
// without re-registering. Empty registry (no NewHTTPFetcher ever
// constructed with a circuit pointer) must return zero samples — the
// /metrics handler relies on that to stay zero-noise on a serve
// process that doesn't run enrichers.
func TestHTTPFetcher_CircuitStateSnapshot(t *testing.T) {
	ResetCircuitStateRegistryForTest()
	t.Cleanup(ResetCircuitStateRegistryForTest)

	// Empty registry → no samples.
	if got := SnapshotCircuitStates(); len(got) != 0 {
		t.Fatalf("empty registry snapshot len = %d, want 0: %+v", len(got), got)
	}

	c, _ := httpx.New(httpx.Options{})
	bdnbFlag := &atomic.Bool{}
	ademeFlag := &atomic.Bool{}
	_ = NewHTTPFetcher(c, HTTPFetcherOptions{ErrPrefix: "bdnb", QuotaTripped: bdnbFlag})
	_ = NewHTTPFetcher(c, HTTPFetcherOptions{ErrPrefix: "ademe", CircuitTripped: ademeFlag})

	// Both registered, both clean.
	snap := SnapshotCircuitStates()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2: %+v", len(snap), snap)
	}
	if snap[0].Source != "ademe" || snap[1].Source != "bdnb" {
		t.Errorf("sources = [%s, %s], want [ademe, bdnb]", snap[0].Source, snap[1].Source)
	}
	if snap[0].Tripped || snap[1].Tripped {
		t.Errorf("clean atomics must yield Tripped=false, got %+v", snap)
	}

	// Flip the BDNB flag — next snapshot must observe the live change
	// without any re-registration call.
	bdnbFlag.Store(true)
	snap = SnapshotCircuitStates()
	wantTripped := map[string]bool{"ademe": false, "bdnb": true}
	for _, s := range snap {
		if s.Tripped != wantTripped[s.Source] {
			t.Errorf("source %s: Tripped = %v, want %v", s.Source, s.Tripped, wantTripped[s.Source])
		}
	}
}

// A nil circuit pointer must NOT register the source (the gauge can't
// expose state for an enricher without a circuit-breaker wired).
func TestHTTPFetcher_CircuitStateSnapshot_NilFlagSkipped(t *testing.T) {
	ResetCircuitStateRegistryForTest()
	t.Cleanup(ResetCircuitStateRegistryForTest)

	c, _ := httpx.New(httpx.Options{})
	_ = NewHTTPFetcher(c, HTTPFetcherOptions{ErrPrefix: "no-breaker"})

	if got := SnapshotCircuitStates(); len(got) != 0 {
		t.Errorf("nil-flag construction must not register: %+v", got)
	}
}

// stubRoundTripper returns a fixed error on every RoundTrip call, used
// to inject transport errors whose Go type is NOT a *net.OpError nor a
// net.Error (e.g. http2.StreamError). The retry layer must classify
// such errors as transport-shaped so the circuit-breaker trips.
type stubRoundTripper struct {
	err   error
	calls int
}

func (s *stubRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	s.calls++
	return nil, s.err
}

// A raw http2.StreamError surfaced from the inner transport must be
// classified as a transport error so the circuit trips after
// MaxConsecutiveTransportErrors. Before the fix, http2.StreamError
// was neither *net.OpError nor net.Error, so isTransportOrDeadlineErr
// returned false and the circuit never tripped — exactly the
// retry-storm observed live against georisques.gouv.fr.
func TestHTTPFetcher_CircuitTripped_OnHTTP2StreamError(t *testing.T) {
	ResetCircuitTripCountersForTest()
	t.Cleanup(ResetCircuitTripCountersForTest)

	stub := &stubRoundTripper{
		err: http2.StreamError{
			StreamID: 601,
			Code:     http2.ErrCodeInternal,
		},
	}
	// MaxRetries: -1 disables the retry loop so each Fetch returns one
	// transport error immediately. The circuit counter must reach 3 in
	// 3 calls.
	c, _ := httpx.New(httpx.Options{MaxRetries: -1, Transport: stub})

	circuit := &atomic.Bool{}
	f := NewHTTPFetcher(c, HTTPFetcherOptions{
		ErrPrefix:                     "georisques",
		CircuitTripped:                circuit,
		MaxConsecutiveTransportErrors: 3,
	})

	for i := 1; i <= 3; i++ {
		_, err := f.Fetch(context.Background(), "https://georisques.gouv.fr/x")
		if err == nil {
			t.Fatalf("call %d: expected error", i)
		}
		if i < 3 && circuit.Load() {
			t.Errorf("circuit tripped early on call %d", i)
		}
	}
	if !circuit.Load() {
		t.Fatal("expected CircuitTripped after 3 consecutive http2.StreamError responses")
	}
}

// Same as above but with the retry layer enabled — verifies that the
// http2.StreamError wrapped in *url.Error / *ErrTooManyRetries /
// *ErrTransport still classifies as transport so the circuit trips
// after the retry budget is exhausted on each Fetch.
func TestHTTPFetcher_CircuitTripped_OnHTTP2StreamError_WithRetries(t *testing.T) {
	ResetCircuitTripCountersForTest()
	t.Cleanup(ResetCircuitTripCountersForTest)

	stub := &stubRoundTripper{
		err: http2.StreamError{
			StreamID: 601,
			Code:     http2.ErrCodeInternal,
		},
	}
	// Allow a tiny retry budget with a near-zero backoff so the test
	// stays fast but exercises the full ErrTooManyRetries wrapping path.
	c, _ := httpx.New(httpx.Options{
		MaxRetries:        2,
		BaseRetryInterval: 1 * time.Millisecond,
		BackoffCap:        2 * time.Millisecond,
		Transport:         stub,
	})

	circuit := &atomic.Bool{}
	f := NewHTTPFetcher(c, HTTPFetcherOptions{
		ErrPrefix:                     "georisques",
		CircuitTripped:                circuit,
		MaxConsecutiveTransportErrors: 3,
	})

	for range 3 {
		_, _ = f.Fetch(context.Background(), "https://georisques.gouv.fr/x")
	}
	if !circuit.Load() {
		t.Fatal("expected CircuitTripped after 3 consecutive ErrTooManyRetries{*ErrTransport{http2.StreamError}}")
	}
}

// Two distinct sources that each trip their own atomic must both
// surface in the snapshot, sorted by source name.
func TestHTTPFetcher_CircuitTripCounter_PerSource(t *testing.T) {
	ResetCircuitTripCountersForTest()
	t.Cleanup(ResetCircuitTripCountersForTest)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
	}))
	defer srv.Close()

	c, _ := httpx.New(httpx.Options{MaxRetries: -1})
	for _, src := range []string{"zeta", "alpha"} {
		flag := &atomic.Bool{}
		f := NewHTTPFetcher(c, HTTPFetcherOptions{ErrPrefix: src, QuotaTripped: flag})
		_, _ = f.Fetch(context.Background(), srv.URL)
	}
	snap := SnapshotCircuitTripCounts()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2: %+v", len(snap), snap)
	}
	// Sorted alphabetically: alpha then zeta.
	if snap[0].Source != "alpha" || snap[1].Source != "zeta" {
		t.Errorf("sources = [%s, %s], want [alpha, zeta]", snap[0].Source, snap[1].Source)
	}
}

func TestTransportCircuit_TripsOnConsecutiveErrors(t *testing.T) {
	flag := &atomic.Bool{}
	tc := NewTransportCircuit("tcsource", 3, flag, nil)

	// 2 transport errors — below threshold.
	tc.Observe(context.DeadlineExceeded)
	tc.Observe(&httpx.ErrTransport{URL: "https://x", Err: errors.New("connection reset")})
	if tc.Tripped() {
		t.Fatalf("tripped at 2, want >=3")
	}

	// Third tick → flip.
	tc.Observe(context.DeadlineExceeded)
	if !tc.Tripped() {
		t.Fatalf("not tripped after 3 errors")
	}
	if !flag.Load() {
		t.Fatalf("flag not flipped")
	}
}

func TestTransportCircuit_ResetsOnSuccess(t *testing.T) {
	flag := &atomic.Bool{}
	tc := NewTransportCircuit("tcreset", 3, flag, nil)

	tc.Observe(context.DeadlineExceeded)
	tc.Observe(context.DeadlineExceeded)
	tc.Observe(nil) // success resets the streak.
	tc.Observe(context.DeadlineExceeded)
	if tc.Tripped() {
		t.Fatalf("tripped after success reset (1 deadline ≠ threshold)")
	}
}

func TestTransportCircuit_IgnoresNonTransportErrors(t *testing.T) {
	flag := &atomic.Bool{}
	tc := NewTransportCircuit("tcignore", 2, flag, nil)

	// 4xx-style application errors do NOT count.
	tc.Observe(errors.New("HTTP 404"))
	tc.Observe(errors.New("HTTP 404"))
	tc.Observe(errors.New("HTTP 404"))
	if tc.Tripped() {
		t.Fatalf("tripped on non-transport errors")
	}
}

func TestTransportCircuit_NilSafe(t *testing.T) {
	var tc *TransportCircuit
	tc.Observe(context.DeadlineExceeded)
	if tc.Tripped() {
		t.Fatalf("nil receiver should never report tripped")
	}
	// SetMax429 on nil receiver is a safe no-op.
	tc.SetMax429(3)
	// Same with a non-nil receiver but nil flag.
	tc = NewTransportCircuit("nilflag", 1, nil, nil)
	tc.Observe(context.DeadlineExceeded)
	if tc.Tripped() {
		t.Fatalf("nil flag should never report tripped")
	}
}

// Three consecutive 429 responses (each already retry-exhausted by
// httpx) must flip the shared atomic when SetMax429(3) is enabled.
// The 429 is wrapped exactly as the retry transport surfaces it in
// production: *ErrTooManyRetries → *ErrHTTP{Status: 429}.
func TestTransportCircuit_TripsOnConsecutive429(t *testing.T) {
	flag := &atomic.Bool{}
	tc := NewTransportCircuit("tc429", 5 /* transport threshold, unused */, flag, nil)
	tc.SetMax429(3)

	wrap429 := func() error {
		return &httpx.ErrTooManyRetries{
			URL:      "https://x",
			Attempts: 6,
			Err:      &httpx.ErrHTTP{Status: http.StatusTooManyRequests, URL: "https://x"},
		}
	}

	tc.Observe(wrap429())
	if tc.Tripped() {
		t.Fatalf("tripped on first 429, want only after 3rd")
	}
	tc.Observe(wrap429())
	if tc.Tripped() {
		t.Fatalf("tripped on second 429, want only after 3rd")
	}
	tc.Observe(wrap429())
	if !tc.Tripped() {
		t.Fatalf("not tripped after 3 consecutive 429s")
	}
	if !flag.Load() {
		t.Fatalf("flag not flipped")
	}
}

// A 2xx between 429s must reset the 429 streak — the breaker must NOT
// fire when 429s are interleaved with successful responses, only when
// the streak is sustained.
func TestTransportCircuit_Reset429StreakOnSuccess(t *testing.T) {
	flag := &atomic.Bool{}
	tc := NewTransportCircuit("tc429reset", 5, flag, nil)
	tc.SetMax429(3)

	wrap429 := func() error {
		return &httpx.ErrHTTP{Status: http.StatusTooManyRequests, URL: "https://x"}
	}

	tc.Observe(wrap429())
	tc.Observe(nil) // success resets the 429 streak.
	tc.Observe(wrap429())
	tc.Observe(nil)
	tc.Observe(wrap429())
	if tc.Tripped() {
		t.Fatalf("tripped despite intervening 2xx successes")
	}
}

// SetMax429(0) keeps the 429 tracker disabled — a flood of 429s must
// not flip the flag. This is the back-compat path for callers that
// only want the transport-error breaker.
func TestTransportCircuit_NoTripOn429WhenDisabled(t *testing.T) {
	flag := &atomic.Bool{}
	tc := NewTransportCircuit("tc429disabled", 5, flag, nil)
	// SetMax429 not called → max429 == 0 → tracker disabled.

	wrap429 := func() error {
		return &httpx.ErrHTTP{Status: http.StatusTooManyRequests, URL: "https://x"}
	}
	for range 10 {
		tc.Observe(wrap429())
	}
	if tc.Tripped() {
		t.Fatalf("tripped on 429 with tracker disabled")
	}
}

// HTTPFetcher with MaxConsecutive429=3 must NOT trip on the first
// 429 (a change from the historic "trip on first 429" semantic, which
// stays the default at MaxConsecutive429==0).
func TestHTTPFetcher_QuotaTripped_RequiresMaxConsecutive429(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
	}))
	defer srv.Close()

	tripped := &atomic.Bool{}
	c, _ := httpx.New(httpx.Options{MaxRetries: -1})
	f := NewHTTPFetcher(c, HTTPFetcherOptions{
		ErrPrefix:         "demo429",
		QuotaTripped:      tripped,
		MaxConsecutive429: 3,
	})
	// First and second 429s must NOT trip yet.
	_, _ = f.Fetch(context.Background(), srv.URL)
	if tripped.Load() {
		t.Fatalf("tripped on first 429 with MaxConsecutive429=3")
	}
	_, _ = f.Fetch(context.Background(), srv.URL)
	if tripped.Load() {
		t.Fatalf("tripped on second 429 with MaxConsecutive429=3")
	}
	// Third 429 must flip the atomic.
	_, _ = f.Fetch(context.Background(), srv.URL)
	if !tripped.Load() {
		t.Fatalf("not tripped after 3 consecutive 429s")
	}
}

// A run of non-transient 4xx / 5xx responses (404, 410, 500) must NOT
// trip the circuit breaker — the classifier only flips on transport /
// deadline errors and on the explicit quota signals (429 +
// x-quota-remaining:0). Tested empirically with a sequence of 10 GETs
// per status code; the breaker must stay closed and the underlying
// *httpx.ErrHTTP must remain reachable so the caller sees the upstream
// failure verbatim.
//
// Regression guard for the user's concern: "if 6 consecutive 404s
// happen early in an enrich run, we don't want the circuit to trip and
// skip the enricher for the rest of the run".
func TestHTTPFetcher_CircuitDoesNotTripOn4xxSequence(t *testing.T) {
	for _, status := range []int{404, 410, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			defer srv.Close()

			circuit := &atomic.Bool{}
			// MaxRetries:-1 keeps each Fetch to a single attempt so the
			// 10-call loop exercises the breaker classifier 10 times
			// without exponential-backoff overhead. A high rate-limit
			// keeps the test sub-second.
			c, _ := httpx.New(httpx.Options{MaxRetries: -1, RateLimitPerHost: 1000, BurstPerHost: 100})
			f := NewHTTPFetcher(c, HTTPFetcherOptions{
				ErrPrefix:                     "demo4xx",
				CircuitTripped:                circuit,
				MaxConsecutiveTransportErrors: 3,
				MaxConsecutive429:             3,
			})

			for i := 1; i <= 10; i++ {
				_, err := f.Fetch(context.Background(), srv.URL)
				if err == nil {
					t.Fatalf("call %d: expected error on %d, got nil", i, status)
				}
				// The 4xx/5xx error must still bubble up as *httpx.ErrHTTP
				// — callers handle 404 as a "no upstream data" signal.
				var herr *httpx.ErrHTTP
				if !errors.As(err, &herr) {
					t.Errorf("call %d: err %v does not wrap *httpx.ErrHTTP", i, err)
					continue
				}
				if herr.Status != status {
					t.Errorf("call %d: got status %d want %d", i, herr.Status, status)
				}
				if circuit.Load() {
					t.Fatalf("circuit tripped after %d %d responses (must never trip on non-transient http errors)", i, status)
				}
			}
		})
	}
}

// A 2xx between 429s resets the streak — three 429s separated by 2xx
// must not flip the flag (only a sustained run does).
func TestHTTPFetcher_QuotaTripped_429StreakResetsOn2xx(t *testing.T) {
	var (
		mu         sync.Mutex
		patternIdx int
	)
	pattern := []int{429, 200, 429, 200, 429}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		var code int
		if patternIdx < len(pattern) {
			code = pattern[patternIdx]
			patternIdx++
		} else {
			code = 200
		}
		mu.Unlock()
		w.WriteHeader(code)
		if code == 200 {
			_, _ = w.Write([]byte("ok"))
		}
	}))
	defer srv.Close()

	tripped := &atomic.Bool{}
	c, _ := httpx.New(httpx.Options{MaxRetries: -1})
	f := NewHTTPFetcher(c, HTTPFetcherOptions{
		ErrPrefix:         "demo429reset",
		QuotaTripped:      tripped,
		MaxConsecutive429: 3,
	})
	for range pattern {
		_, _ = f.Fetch(context.Background(), srv.URL)
	}
	if tripped.Load() {
		t.Fatalf("tripped despite intervening 2xx successes")
	}
}
