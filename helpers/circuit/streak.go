package circuit

import (
	"log/slog"
	"sync/atomic"
)

// ContentValidator inspects a response body and reports whether it is
// "signal-bearing". A return value of true means the body carries the
// upstream content the caller expects; false means the body is empty
// (paywall stub, anti-bot interstitial, server stale-cache placeholder,
// missing block, etc.).
//
// Validators must be pure: they may not mutate the body, allocate
// persistent state, or touch shared globals. The breaker invokes the
// validator once per Observe call from arbitrary goroutines.
//
// A nil validator is treated as "every non-empty body is signal-bearing"
// — useful for tests and as a safe default.
type ContentValidator func(body []byte) bool

// ContentStreakBreaker trips a shared *atomic.Bool after N consecutive
// "empty content" responses with no intervening signal-bearing response.
// It complements the transport-error breakers in this package:
// TransportCircuit / HTTPFetcher trip on HTTP-level failures (transport
// errors, deadlines, 429s); ContentStreakBreaker trips on the harder
// "HTTP 200 with no useful body" failure mode that those breakers cannot
// see.
//
// Common triggers:
//
//   - paywall returning 200 with a sign-in stub instead of the article;
//   - anti-bot interstitial cached behind a CDN, returning 200 with a
//     challenge page;
//   - upstream stale cache serving a placeholder when the real backend
//     is down.
//
// Construction is via NewContentStreakBreaker; Observe folds one outcome
// into the counter. A nil receiver is a no-op so callers can opt-out by
// passing a nil pointer.
type ContentStreakBreaker struct {
	source    string
	threshold int
	logger    *slog.Logger
	flag      *atomic.Bool
	consec    atomic.Int32
}

// NewContentStreakBreaker returns a ContentStreakBreaker bound to flag.
// Parameters:
//
//   - source is the label used for the metrics gauge and the trip log
//     line. An empty value is normalised to "common" so the snapshot
//     never carries a blank key. Sharing a source label with an existing
//     TransportCircuit / HTTPFetcher is allowed: the registry stores the
//     last registered pointer and both breakers can flip the same flag
//     (typical wiring — one shared *atomic.Bool per upstream).
//   - threshold is the number of consecutive empty-content observations
//     required to trip the flag. A non-positive value disables the
//     counter (every Observe is a no-op).
//   - flag is the shared *atomic.Bool the breaker flips on trip. A nil
//     value yields a no-op breaker; callers check flag.Load() before
//     scheduling further work.
//   - logger receives a single Warn line at the moment the streak
//     threshold is crossed. Optional: when nil, slog.Default() is used.
//
// The breaker registers its flag in the same process-wide state map as
// TransportCircuit so SnapshotCircuitStates surfaces a unified gauge per
// upstream regardless of which breaker tripped it.
func NewContentStreakBreaker(source string, threshold int, flag *atomic.Bool, logger *slog.Logger) *ContentStreakBreaker {
	if source == "" {
		source = "common"
	}
	b := &ContentStreakBreaker{
		source:    source,
		threshold: threshold,
		logger:    logger,
		flag:      flag,
	}
	if flag != nil {
		registerCircuitState(source, flag)
	}
	return b
}

// Tripped reports whether the underlying flag has been flipped. Safe to
// call on a nil receiver.
func (b *ContentStreakBreaker) Tripped() bool {
	if b == nil || b.flag == nil {
		return false
	}
	return b.flag.Load()
}

// Observe folds one response outcome into the streak counter.
//
// When validator is nil, the body is treated as signal-bearing iff
// len(body) > 0 — a conservative default that prevents accidental trips
// on callers that haven't supplied a real validator yet.
//
// Semantics:
//   - validator(body) == true  → reset the streak to zero.
//   - validator(body) == false → increment the streak; if it reaches the
//     threshold, flip the shared atomic (idempotent: only the first
//     transition is counted by the metrics).
//
// Safe to call on a nil receiver, with a nil flag, or with a non-positive
// threshold — each is a no-op.
func (b *ContentStreakBreaker) Observe(body []byte, validator ContentValidator) {
	if b == nil || b.flag == nil || b.threshold <= 0 {
		return
	}
	b.ObserveSignal(b.signalBearing(body, validator))
}

// ObserveSignal is the low-level entry point: signalBearing == true
// resets the streak, false increments it. Use this when the caller has
// already classified the response (e.g. a parser sentinel like
// ErrBlockMissing) and wants to feed the breaker without re-running a
// validator over the body.
//
// Safe to call on a nil receiver.
func (b *ContentStreakBreaker) ObserveSignal(signalBearing bool) {
	if b == nil || b.flag == nil || b.threshold <= 0 {
		return
	}
	if signalBearing {
		b.consec.Store(0)
		return
	}
	n := b.consec.Add(1)
	if int(n) >= b.threshold {
		tripAndWarn(b.flag, b.source,
			"circuit tripped on consecutive empty-content responses", b.logger,
			slog.Int("consecutive_empty", int(n)),
			slog.Int("threshold", b.threshold),
		)
	}
}

// signalBearing returns true when the body should count as carrying real
// upstream content. The nil-validator branch falls back to "any
// non-empty body counts" so callers that haven't written a domain-
// specific validator yet don't get spurious trips from short HTML stubs.
func (b *ContentStreakBreaker) signalBearing(body []byte, validator ContentValidator) bool {
	if validator == nil {
		return len(body) > 0
	}
	return validator(body)
}

// ConsecutiveEmpty returns the current run of consecutive empty-content
// observations since the last signal-bearing response. Exported for
// tests and metrics surfaces that want to display the streak alongside
// the tripped gauge. Safe to call on a nil receiver.
func (b *ContentStreakBreaker) ConsecutiveEmpty() int {
	if b == nil {
		return 0
	}
	return int(b.consec.Load())
}
