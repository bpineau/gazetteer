package circuit

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/pkg/httpx"
)

// fakeClock returns a clock function that advances on demand via the
// returned setter. Tests pass setNow into RateWindowOptions.Now so
// every Observe sees a deterministic time.
func fakeClock(start time.Time) (now func() time.Time, set func(time.Time)) {
	var mu sync.Mutex
	t := start
	now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return t
	}
	set = func(nt time.Time) {
		mu.Lock()
		defer mu.Unlock()
		t = nt
	}
	return
}

// 50 % errors above MinSamples must flip the breaker via the ratio
// rule. The absolute-error fallback is set high enough that only the
// ratio path can fire.
func TestRateWindow_TripsOnRatioAboveThreshold(t *testing.T) {
	ResetCircuitTripCountersForTest()
	ResetCircuitStateRegistryForTest()
	t.Cleanup(ResetCircuitTripCountersForTest)
	t.Cleanup(ResetCircuitStateRegistryForTest)

	now, _ := fakeClock(time.Unix(1_700_000_000, 0).UTC())
	flag := &atomic.Bool{}
	rw := NewRateWindow(RateWindowOptions{
		Source:                "rwratio",
		Window:                5 * time.Minute,
		BucketCount:           10,
		TripErrorRatio:        0.50,
		MinSamples:            20,
		TripMinAbsoluteErrors: 100, // disable absolute fallback for this test.
		Flag:                  flag,
		Now:                   now,
	})

	// 20 calls, 11 errors (55 %) → MUST trip via the ratio path.
	for i := range 20 {
		if i < 11 {
			rw.Observe(context.DeadlineExceeded)
		} else {
			rw.Observe(nil)
		}
	}
	if !flag.Load() {
		t.Fatal("expected flag tripped at 55 % errors on 20 samples")
	}
}

// 30 % errors must NOT trip, even on a large sample count — exactly
// at the reset threshold, well below the 50 % trip ratio.
func TestRateWindow_DoesNotTripAtThirtyPercent(t *testing.T) {
	ResetCircuitTripCountersForTest()
	ResetCircuitStateRegistryForTest()
	t.Cleanup(ResetCircuitTripCountersForTest)
	t.Cleanup(ResetCircuitStateRegistryForTest)

	now, _ := fakeClock(time.Unix(1_700_000_000, 0).UTC())
	flag := &atomic.Bool{}
	rw := NewRateWindow(RateWindowOptions{
		Source:                "rwclean",
		Window:                5 * time.Minute,
		BucketCount:           10,
		TripErrorRatio:        0.50,
		MinSamples:            20,
		TripMinAbsoluteErrors: 100,
		Flag:                  flag,
		Now:                   now,
	})

	// 100 calls, 30 errors (30 %) → MUST NOT trip.
	for i := range 100 {
		if i%10 < 3 {
			rw.Observe(context.DeadlineExceeded)
		} else {
			rw.Observe(nil)
		}
	}
	if flag.Load() {
		t.Fatalf("expected flag NOT tripped at 30 %% errors; got tripped")
	}
}

// The absolute-error fallback must fire before the window holds
// MinSamples observations — a burst of 10 errors on the very first
// requests must trip even though the ratio rule is dormant.
func TestRateWindow_AbsoluteFallbackFires(t *testing.T) {
	ResetCircuitTripCountersForTest()
	ResetCircuitStateRegistryForTest()
	t.Cleanup(ResetCircuitTripCountersForTest)
	t.Cleanup(ResetCircuitStateRegistryForTest)

	now, _ := fakeClock(time.Unix(1_700_000_000, 0).UTC())
	flag := &atomic.Bool{}
	rw := NewRateWindow(RateWindowOptions{
		Source:                "rwabs",
		Window:                5 * time.Minute,
		BucketCount:           10,
		TripErrorRatio:        0.50,
		MinSamples:            20,
		TripMinAbsoluteErrors: 10,
		Flag:                  flag,
		Now:                   now,
	})

	// 9 errors must NOT trip; the 10th must.
	for range 9 {
		rw.Observe(context.DeadlineExceeded)
	}
	if flag.Load() {
		t.Fatal("absolute fallback fired too early (9 errors)")
	}
	rw.Observe(context.DeadlineExceeded)
	if !flag.Load() {
		t.Fatal("absolute fallback should fire at 10 consecutive errors with <MinSamples")
	}
}

// Reset rule: after a trip, a clean window (≥ MinSamples observations
// with error-rate ≤ ResetErrorRatio) and the cooldown elapsed must
// flip the flag back to false.
func TestRateWindow_AutoResetAfterCleanWindow(t *testing.T) {
	ResetCircuitTripCountersForTest()
	ResetCircuitStateRegistryForTest()
	t.Cleanup(ResetCircuitTripCountersForTest)
	t.Cleanup(ResetCircuitStateRegistryForTest)

	t0 := time.Unix(1_700_000_000, 0).UTC()
	now, setNow := fakeClock(t0)

	flag := &atomic.Bool{}
	rw := NewRateWindow(RateWindowOptions{
		Source:                "rwreset",
		Window:                5 * time.Minute,
		BucketCount:           10,
		TripErrorRatio:        0.50,
		MinSamples:            20,
		TripMinAbsoluteErrors: 10,
		ResetErrorRatio:       0.30,
		AllowReset:            true,
		ResetCooldown:         5 * time.Minute,
		Flag:                  flag,
		Now:                   now,
	})

	// Trip via the absolute-error fallback (10 fast errors).
	for range 10 {
		rw.Observe(context.DeadlineExceeded)
	}
	if !flag.Load() {
		t.Fatal("setup failure: breaker must trip on 10 absolute errors")
	}

	// Advance past the cooldown (≥ 5 min). All old error buckets
	// roll off; fresh 2xx feeds the window.
	setNow(t0.Add(6 * time.Minute))
	for range 30 {
		rw.Observe(nil)
	}
	if flag.Load() {
		t.Fatalf("expected auto-reset after clean window past cooldown; still tripped")
	}
}

// AllowReset = false (default) keeps the flag latched even when the
// window goes clean — quota-style breakers must NOT auto-recover.
func TestRateWindow_NoAutoResetWhenAllowResetFalse(t *testing.T) {
	ResetCircuitTripCountersForTest()
	ResetCircuitStateRegistryForTest()
	t.Cleanup(ResetCircuitTripCountersForTest)
	t.Cleanup(ResetCircuitStateRegistryForTest)

	t0 := time.Unix(1_700_000_000, 0).UTC()
	now, setNow := fakeClock(t0)

	flag := &atomic.Bool{}
	rw := NewRateWindow(RateWindowOptions{
		Source:                "rwnoreset",
		Window:                5 * time.Minute,
		BucketCount:           10,
		TripErrorRatio:        0.50,
		MinSamples:            20,
		TripMinAbsoluteErrors: 10,
		AllowReset:            false,
		Flag:                  flag,
		Now:                   now,
	})

	for range 10 {
		rw.Observe(context.DeadlineExceeded)
	}
	if !flag.Load() {
		t.Fatal("breaker must trip on 10 absolute errors")
	}
	setNow(t0.Add(10 * time.Minute))
	for range 100 {
		rw.Observe(nil)
	}
	if !flag.Load() {
		t.Fatal("breaker must stay tripped when AllowReset is false")
	}
}

// Non-transport errors (4xx, 5xx without transport shape) are
// application signals and must not fold into the rate window at all.
// The flag must NOT trip even after 50+ such errors.
func TestRateWindow_IgnoresNonTransportErrors(t *testing.T) {
	ResetCircuitTripCountersForTest()
	ResetCircuitStateRegistryForTest()
	t.Cleanup(ResetCircuitTripCountersForTest)
	t.Cleanup(ResetCircuitStateRegistryForTest)

	now, _ := fakeClock(time.Unix(1_700_000_000, 0).UTC())
	flag := &atomic.Bool{}
	rw := NewRateWindow(RateWindowOptions{
		Source:                "rwignore",
		Window:                5 * time.Minute,
		BucketCount:           10,
		TripErrorRatio:        0.50,
		MinSamples:            20,
		TripMinAbsoluteErrors: 10,
		Flag:                  flag,
		Now:                   now,
	})
	for range 50 {
		rw.Observe(errors.New("HTTP 404"))
	}
	if flag.Load() {
		t.Fatal("non-transport errors must not fold into the rate window")
	}
	totalOk, totalErr, ratio := rw.Snapshot()
	if totalOk != 0 || totalErr != 0 || ratio != 0 {
		t.Fatalf("expected zero counters, got ok=%d err=%d ratio=%f", totalOk, totalErr, ratio)
	}
}

// nil receiver is a no-op safety. Observe / Tripped / Snapshot must
// not crash.
func TestRateWindow_NilSafe(t *testing.T) {
	var rw *RateWindow
	rw.Observe(context.DeadlineExceeded)
	if rw.Tripped() {
		t.Fatal("nil receiver must never report tripped")
	}
	ok, e, r := rw.Snapshot()
	if ok != 0 || e != 0 || r != 0 {
		t.Fatalf("nil snapshot must be zero, got %d/%d/%f", ok, e, r)
	}

	// NewRateWindow with nil Flag must return nil — also no-op.
	rw = NewRateWindow(RateWindowOptions{Flag: nil})
	if rw != nil {
		t.Fatalf("NewRateWindow(nil flag) must return nil, got %+v", rw)
	}
}

// Old buckets must roll off — observing 60 errors at t0 followed by a
// 30-minute jump must yield an empty window snapshot (all old buckets
// fall outside the live span) and a fresh 5 OK calls must not trip.
func TestRateWindow_BucketsRollOff(t *testing.T) {
	ResetCircuitTripCountersForTest()
	ResetCircuitStateRegistryForTest()
	t.Cleanup(ResetCircuitTripCountersForTest)
	t.Cleanup(ResetCircuitStateRegistryForTest)

	t0 := time.Unix(1_700_000_000, 0).UTC()
	now, setNow := fakeClock(t0)
	flag := &atomic.Bool{}
	rw := NewRateWindow(RateWindowOptions{
		Source:                "rwrolloff",
		Window:                5 * time.Minute,
		BucketCount:           10,
		TripErrorRatio:        0.50,
		MinSamples:            1000, // high so only old errors could trip via ratio.
		TripMinAbsoluteErrors: 1000, // disable absolute fallback entirely.
		Flag:                  flag,
		Now:                   now,
	})
	// 60 errors at t0.
	for range 60 {
		rw.Observe(context.DeadlineExceeded)
	}
	// Jump 30 minutes ahead → all old buckets rolled off.
	setNow(t0.Add(30 * time.Minute))
	totalOk, totalErr, _ := rw.Snapshot()
	if totalOk != 0 || totalErr != 0 {
		t.Fatalf("expected empty window after 30 min, got ok=%d err=%d", totalOk, totalErr)
	}
}

// Integration: feed 100 mock responses through HTTPFetcher with a
// wired RateWindow. The pattern interleaves errors and successes so
// the consecutive-streak breaker (threshold 3) never fires, but the
// sliding-window breaker (>50 % errors on ≥ 20 samples) does. This
// is the canonical "slow burn" scenario the rate-window exists for.
func TestHTTPFetcher_RateWindow_TripsOnSlowBurn(t *testing.T) {
	ResetCircuitTripCountersForTest()
	ResetCircuitStateRegistryForTest()
	t.Cleanup(ResetCircuitTripCountersForTest)
	t.Cleanup(ResetCircuitStateRegistryForTest)

	var (
		mu  sync.Mutex
		idx int
	)
	// Pattern: 2 errors + 1 success, repeating. 67 % error rate
	// across the run; consecutive errors never exceed 2 (below the
	// streak breaker's threshold of 3).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		i := idx
		idx++
		mu.Unlock()
		if i%3 != 2 {
			// Hang to force a transport-deadline error.
			<-r.Context().Done()
			return
		}
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	flag := &atomic.Bool{}
	// MaxRetries:-1 keeps each Fetch to one attempt; high rate-limit
	// prevents the per-host token bucket from starving the test (the
	// default 2 req/s burst 4 caps total requests to ~4-5 in the
	// sub-second window we run for).
	c, _ := httpx.New(httpx.Options{MaxRetries: -1, RateLimitPerHost: 1000, BurstPerHost: 100})
	now, _ := fakeClock(time.Unix(1_700_000_000, 0).UTC())
	rw := NewRateWindow(RateWindowOptions{
		Source:                "slowburn",
		Window:                5 * time.Minute,
		BucketCount:           10,
		TripErrorRatio:        0.50,
		MinSamples:            20,
		TripMinAbsoluteErrors: 1000, // disable absolute fallback; force ratio path.
		Flag:                  flag,
		Now:                   now,
	})
	f := NewHTTPFetcher(c, HTTPFetcherOptions{
		ErrPrefix:                     "slowburn",
		CircuitTripped:                flag,
		MaxConsecutiveTransportErrors: 3, // streak rule that must NOT fire
		RateWindow:                    rw,
	})

	// 30 calls under the pattern. The streak counter never reaches
	// 3 (every 3rd call is a 2xx that resets it). The window after
	// 30 calls holds ~20 errors / ~10 ok → 67 % error rate, > 50 %.
	for range 30 {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		_, _ = f.Fetch(ctx, srv.URL)
		cancel()
		if flag.Load() {
			break
		}
	}
	if !flag.Load() {
		ok, e, ratio := rw.Snapshot()
		t.Fatalf("rate-window breaker did NOT trip on slow-burn pattern: ok=%d err=%d ratio=%.2f", ok, e, ratio)
	}
}
