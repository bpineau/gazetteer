package circuit

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestContentStreakBreaker_BelowThreshold_DoesNotTrip pins that fewer
// than threshold consecutive empty-content observations leave the flag
// untouched.
func TestContentStreakBreaker_BelowThreshold_DoesNotTrip(t *testing.T) {
	ResetCircuitStateRegistryForTest()
	ResetCircuitTripCountersForTest()

	var flag atomic.Bool
	b := NewContentStreakBreaker("demo", 3, &flag, nil)

	v := func(body []byte) bool { return len(body) > 0 }

	b.Observe(nil, v)
	b.Observe(nil, v)
	if flag.Load() {
		t.Fatalf("flag tripped at %d observations want stay false (threshold=3)", b.ConsecutiveEmpty())
	}
	if got := b.ConsecutiveEmpty(); got != 2 {
		t.Errorf("ConsecutiveEmpty = %d want 2", got)
	}
}

// TestContentStreakBreaker_AtThreshold_Trips pins that exactly threshold
// consecutive empty observations flip the flag.
func TestContentStreakBreaker_AtThreshold_Trips(t *testing.T) {
	ResetCircuitStateRegistryForTest()
	ResetCircuitTripCountersForTest()

	var flag atomic.Bool
	b := NewContentStreakBreaker("demo", 3, &flag, nil)
	v := func(body []byte) bool { return len(body) > 0 }

	b.Observe(nil, v)
	b.Observe(nil, v)
	b.Observe(nil, v)
	if !flag.Load() {
		t.Fatalf("flag not tripped at threshold (consec=%d)", b.ConsecutiveEmpty())
	}

	// Trip counter recorded exactly once for the false→true transition.
	got := SnapshotCircuitTripCounts()
	if len(got) != 1 || got[0].Source != "demo" || got[0].Count != 1 {
		t.Errorf("trip counters = %+v want [{demo 1}]", got)
	}
}

// TestContentStreakBreaker_SignalBearing_ResetsStreak pins that a single
// signal-bearing response resets the consecutive-empty counter to zero,
// so the breaker only trips on a sustained streak.
func TestContentStreakBreaker_SignalBearing_ResetsStreak(t *testing.T) {
	ResetCircuitStateRegistryForTest()
	ResetCircuitTripCountersForTest()

	var flag atomic.Bool
	b := NewContentStreakBreaker("demo", 3, &flag, nil)
	v := func(body []byte) bool { return len(body) > 0 }

	b.Observe(nil, v)
	b.Observe(nil, v)
	if got := b.ConsecutiveEmpty(); got != 2 {
		t.Fatalf("ConsecutiveEmpty before reset = %d want 2", got)
	}
	b.Observe([]byte("ok"), v)
	if got := b.ConsecutiveEmpty(); got != 0 {
		t.Fatalf("ConsecutiveEmpty after signal = %d want 0", got)
	}
	// Two more empties should not trip — streak restarted from zero.
	b.Observe(nil, v)
	b.Observe(nil, v)
	if flag.Load() {
		t.Errorf("flag tripped after reset (consec=%d, threshold=3)", b.ConsecutiveEmpty())
	}
}

// TestContentStreakBreaker_NilValidator_FallbackToLen pins that with no
// validator the breaker treats len(body) > 0 as signal-bearing.
func TestContentStreakBreaker_NilValidator_FallbackToLen(t *testing.T) {
	ResetCircuitStateRegistryForTest()
	ResetCircuitTripCountersForTest()

	var flag atomic.Bool
	b := NewContentStreakBreaker("demo", 2, &flag, nil)

	// Non-empty body counts as signal-bearing with nil validator.
	b.Observe([]byte("anything"), nil)
	b.Observe([]byte("anything"), nil)
	if flag.Load() {
		t.Fatalf("flag tripped on non-empty bodies (nil validator)")
	}

	// Empty bodies bump the streak.
	b.Observe(nil, nil)
	b.Observe(nil, nil)
	if !flag.Load() {
		t.Errorf("flag not tripped after 2 empty bodies (threshold=2)")
	}
}

// TestContentStreakBreaker_NilReceiver_NoOp pins that a nil breaker is a
// safe sink — callers that haven't wired a breaker yet can still call
// Observe / Tripped / ConsecutiveEmpty without panicking.
func TestContentStreakBreaker_NilReceiver_NoOp(t *testing.T) {
	var b *ContentStreakBreaker
	if b.Tripped() {
		t.Errorf("nil breaker reports tripped")
	}
	if got := b.ConsecutiveEmpty(); got != 0 {
		t.Errorf("nil breaker ConsecutiveEmpty = %d want 0", got)
	}
	b.Observe(nil, nil)    // must not panic
	b.ObserveSignal(false) // must not panic
}

// TestContentStreakBreaker_NilFlag_NoOp pins that a non-nil breaker
// constructed with a nil flag is also a safe no-op (the registry skip
// already verified in NewContentStreakBreaker matches the Observe
// branch).
func TestContentStreakBreaker_NilFlag_NoOp(t *testing.T) {
	b := NewContentStreakBreaker("demo", 1, nil, nil)
	b.Observe(nil, nil)
	b.ObserveSignal(false)
	if b.Tripped() {
		t.Errorf("breaker with nil flag reports tripped")
	}
}

// TestContentStreakBreaker_NonPositiveThreshold_NoOp pins that a
// non-positive threshold disables the counter — useful for "wire the
// shape, defer the policy" wiring sites.
func TestContentStreakBreaker_NonPositiveThreshold_NoOp(t *testing.T) {
	ResetCircuitStateRegistryForTest()
	ResetCircuitTripCountersForTest()

	var flag atomic.Bool
	b := NewContentStreakBreaker("demo", 0, &flag, nil)
	for range 100 {
		b.Observe(nil, nil)
	}
	if flag.Load() {
		t.Errorf("flag tripped with threshold=0")
	}
}

// TestContentStreakBreaker_TripOnceOnly pins that crossing the threshold
// multiple times in a row only records ONE trip count — the metrics
// counter is monotonic per false→true transition, not per Observe.
func TestContentStreakBreaker_TripOnceOnly(t *testing.T) {
	ResetCircuitStateRegistryForTest()
	ResetCircuitTripCountersForTest()

	var flag atomic.Bool
	b := NewContentStreakBreaker("demo", 2, &flag, nil)

	// Trip the breaker, then keep observing empties. The trip counter
	// must not climb past 1.
	for range 10 {
		b.Observe(nil, nil)
	}
	if !flag.Load() {
		t.Fatalf("flag not tripped after sustained empties")
	}
	got := SnapshotCircuitTripCounts()
	if len(got) != 1 || got[0].Count != 1 {
		t.Errorf("trip counters = %+v want exactly one transition", got)
	}
}

// TestContentStreakBreaker_RegistersInStateSnapshot pins that the flag
// shows up in SnapshotCircuitStates — the gauge counterpart to the
// monotonic trip counter, shared with TransportCircuit / HTTPFetcher.
func TestContentStreakBreaker_RegistersInStateSnapshot(t *testing.T) {
	ResetCircuitStateRegistryForTest()
	ResetCircuitTripCountersForTest()

	var flag atomic.Bool
	_ = NewContentStreakBreaker("demo-streak", 1, &flag, nil)

	states := SnapshotCircuitStates()
	found := false
	for _, s := range states {
		if s.Source == "demo-streak" {
			found = true
			if s.Tripped {
				t.Errorf("state.Tripped = true before any Observe")
			}
		}
	}
	if !found {
		t.Errorf("source 'demo-streak' missing from SnapshotCircuitStates = %+v", states)
	}
}

// TestContentStreakBreaker_ObserveSignal pins the low-level entry point
// that bypasses the validator — callers with parser-sentinel info pass
// the signal-bearing classification directly.
func TestContentStreakBreaker_ObserveSignal(t *testing.T) {
	ResetCircuitStateRegistryForTest()
	ResetCircuitTripCountersForTest()

	var flag atomic.Bool
	b := NewContentStreakBreaker("demo", 3, &flag, nil)

	b.ObserveSignal(false)
	b.ObserveSignal(false)
	if flag.Load() {
		t.Fatalf("flag tripped before threshold")
	}
	b.ObserveSignal(true) // resets
	b.ObserveSignal(false)
	b.ObserveSignal(false)
	if flag.Load() {
		t.Fatalf("flag tripped after reset")
	}
	b.ObserveSignal(false)
	if !flag.Load() {
		t.Errorf("flag not tripped after 3 consecutive empty (post-reset)")
	}
}

// TestContentStreakBreaker_Concurrent_TripOnce pins that under
// concurrent Observe calls the breaker still records exactly one
// false→true transition. The atomic CompareAndSwap in Observe is the
// only synchronisation point.
func TestContentStreakBreaker_Concurrent_TripOnce(t *testing.T) {
	ResetCircuitStateRegistryForTest()
	ResetCircuitTripCountersForTest()

	var flag atomic.Bool
	b := NewContentStreakBreaker("demo", 5, &flag, nil)

	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			b.Observe(nil, nil)
		})
	}
	wg.Wait()

	if !flag.Load() {
		t.Fatalf("flag not tripped after 100 concurrent empty observations")
	}
	got := SnapshotCircuitTripCounts()
	if len(got) != 1 || got[0].Count != 1 {
		t.Errorf("trip counters = %+v want exactly one transition", got)
	}
}
