package circuit

import (
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// RateWindow is a sliding-window error-rate circuit breaker. It
// complements the consecutive-streak breakers built into HTTPFetcher /
// TransportCircuit by catching "slow burn" failure modes that the
// streak counters miss: 30-60 % of requests failing for several
// minutes in a row, interleaved with enough 2xx to keep the consecutive
// counter pinned at zero.
//
// # Algorithm
//
//   - The window is split into fixed-size buckets (default: 10 buckets
//     of 30 s = 5 min window). Each bucket stores (ok, err) counters.
//   - Observe(err) bumps the current bucket. The bucket is selected by
//     `(now.Unix() / bucketSeconds) % numBuckets` and rotated lazily —
//     when a bucket's epoch advances we reset its counters before
//     incrementing.
//   - On every Observe, the window totals are summed across all buckets.
//     If totalErr / (totalErr + totalOk) > TripErrorRatio and the
//     window holds ≥ MinSamples requests, the circuit flips. The
//     fallback rule "≥ TripMinAbsoluteErrors errors when MinSamples not
//     yet reached" guards against tripping on the very first error
//     when the window is otherwise empty (false-positive on a single
//     blip).
//   - When AllowReset is set, a clean window (error-rate ≤
//     ResetErrorRatio across the full window) flips the flag back to
//     false. ResetErrorRatio defaults to 30 %. Reset is opt-in because
//     quota-style breakers (BDNB, ADEME) must stay tripped for the
//     rest of the run — only transient-transport breakers want auto-
//     recovery.
//
// # Wiring
//
//   - HTTPFetcher consumes a RateWindow via HTTPFetcherOptions.RateWindow.
//     Every Fetch call feeds the window: 2xx → Observe(nil), transport /
//     deadline failure → Observe(theErr). Non-transport errors (4xx,
//     5xx without transport shape) are ignored — they're application
//     signals, not infrastructure failures.
//   - A caller using TransportCircuit (or any custom path) can drive a
//     RateWindow directly: construct one sharing the same *atomic.Bool
//     flag and call its Observe alongside the TransportCircuit's, since
//     both classify err the same way.
//
// The breaker pointer (*atomic.Bool) is shared with HTTPFetcher — the
// rate-window flips the same flag the consecutive-streak counter does.
// Callers check `flag.Load()` once on the hot path; the rate-window's
// machinery is invisible from the outside.
//
// # Defaults
//
// Pass a zero RateWindowOptions and the constructor fills in:
//
//   - Window        : 5 min
//   - BucketCount   : 10 (30 s each)
//   - TripErrorRatio: 0.50 (> 50 % errors in the window → trip)
//   - MinSamples    : 20 (need ≥ 20 requests before the ratio rule fires)
//   - TripMinAbsoluteErrors: 10 (fallback when MinSamples not reached)
//   - ResetErrorRatio: 0.30 (≤ 30 % errors for the full window → reset)
//   - AllowReset    : false (opt-in; off by default for safety)
//
// Defaults were chosen for HTTP-breaker literature norms (5 min window
// is the canonical "Hystrix-style" default).
type RateWindow struct {
	source string

	bucketDur     time.Duration
	bucketSeconds int64
	buckets       []rateBucket
	mu            sync.Mutex

	tripRatio       float64
	minSamples      int
	minAbsErrors    int
	resetRatio      float64
	allowReset      bool
	cooldownSeconds int64

	flag   *atomic.Bool
	logger *slog.Logger

	// now is the clock source. Defaults to time.Now; tests inject a
	// deterministic clock.
	now func() time.Time

	// lastTripUnix holds the Unix-second at which the breaker last
	// flipped from false to true. Used to enforce a cooldown before
	// auto-reset can fire so a fresh trip is not immediately undone by
	// the same buckets it just observed.
	lastTripUnix atomic.Int64
}

// rateBucket is one (epoch, ok, err) cell in the ring buffer. epoch
// holds the bucket's start-of-window second; ok / err count the
// observations folded into that bucket. When epoch advances (the next
// Observe lands in a "future" bucket) the counters are reset.
type rateBucket struct {
	epoch int64
	ok    int64
	err   int64
}

// RateWindowOptions configures a RateWindow. Every field is optional;
// the zero value yields the canonical 5-min / 10-bucket / 50 %-trip /
// 30 %-reset breaker described in the RateWindow doc-comment.
type RateWindowOptions struct {
	// Source labels the breaker in trip-warning logs. Defaults to
	// "common".
	Source string

	// Window is the total span of the sliding rate window. Defaults to
	// 5 min. Must divide evenly by BucketCount (the constructor pads
	// up to the next multiple if it doesn't).
	Window time.Duration

	// BucketCount is the number of fixed-size buckets the window is
	// split into. Defaults to 10. Higher counts trade more memory for
	// finer time resolution; 10 (30 s / bucket on the 5-min default)
	// is a good HTTP-breaker baseline.
	BucketCount int

	// TripErrorRatio is the error-rate threshold above which the
	// circuit flips. Defaults to 0.50. Must be in (0, 1].
	TripErrorRatio float64

	// MinSamples is the minimum number of observations in the window
	// before the ratio rule fires. Below this count, only the absolute
	// fallback (TripMinAbsoluteErrors) can trip the circuit. Defaults
	// to 20.
	MinSamples int

	// TripMinAbsoluteErrors is the absolute-error fallback used while
	// the window holds fewer than MinSamples requests. Defaults to 10.
	// Set to 0 to disable the fallback (only the ratio rule fires).
	TripMinAbsoluteErrors int

	// ResetErrorRatio is the error-rate threshold below which the
	// circuit auto-resets (when AllowReset is true). Defaults to 0.30.
	ResetErrorRatio float64

	// AllowReset, when true, lets the breaker flip back to false once
	// the window's error-rate drops below ResetErrorRatio. Defaults
	// to false: quota-style breakers (BDNB, ADEME) must stay tripped
	// for the rest of the run. Transient-transport breakers
	// (Georisques HTTP/2, Castorus anti-bot) want auto-recovery and
	// should set this to true.
	AllowReset bool

	// ResetCooldown is the minimum time between a trip and the next
	// auto-reset attempt. Defaults to Window: once we trip, we wait
	// at least one full window before considering reset. Prevents
	// the same buckets that triggered the trip from immediately
	// satisfying the reset condition as they roll off.
	ResetCooldown time.Duration

	// Flag is the shared *atomic.Bool the rate-window flips. Mandatory.
	Flag *atomic.Bool

	// Logger is used to surface a single Warn line on every false→true
	// or true→false transition. Optional: nil uses slog.Default().
	Logger *slog.Logger

	// Now, when non-nil, overrides time.Now. Tests inject a
	// deterministic clock; production leaves this nil.
	Now func() time.Time
}

// NewRateWindow constructs a RateWindow with the given options. A nil
// Flag yields a nil RateWindow — the no-op shape, safe to call
// Observe / Tripped on without crashing.
func NewRateWindow(opts RateWindowOptions) *RateWindow {
	if opts.Flag == nil {
		return nil
	}
	if opts.Source == "" {
		opts.Source = "common"
	}
	if opts.Window <= 0 {
		opts.Window = 5 * time.Minute
	}
	if opts.BucketCount <= 0 {
		opts.BucketCount = 10
	}
	// Round the window UP to the nearest multiple of BucketCount so
	// each bucket has an integer second count. Avoids fractional-
	// epoch math on every Observe.
	bucketDur := max(opts.Window/time.Duration(opts.BucketCount), time.Second)
	opts.Window = bucketDur * time.Duration(opts.BucketCount)
	if opts.TripErrorRatio <= 0 || opts.TripErrorRatio > 1 {
		opts.TripErrorRatio = 0.50
	}
	if opts.MinSamples <= 0 {
		opts.MinSamples = 20
	}
	if opts.TripMinAbsoluteErrors < 0 {
		opts.TripMinAbsoluteErrors = 0
	}
	if opts.TripMinAbsoluteErrors == 0 && opts.MinSamples > 0 {
		opts.TripMinAbsoluteErrors = 10
	}
	if opts.ResetErrorRatio <= 0 || opts.ResetErrorRatio > 1 {
		opts.ResetErrorRatio = 0.30
	}
	if opts.ResetCooldown <= 0 {
		opts.ResetCooldown = opts.Window
	}
	now := opts.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	rw := &RateWindow{
		source:          opts.Source,
		bucketDur:       bucketDur,
		bucketSeconds:   int64(bucketDur / time.Second),
		buckets:         make([]rateBucket, opts.BucketCount),
		tripRatio:       opts.TripErrorRatio,
		minSamples:      opts.MinSamples,
		minAbsErrors:    opts.TripMinAbsoluteErrors,
		resetRatio:      opts.ResetErrorRatio,
		allowReset:      opts.AllowReset,
		cooldownSeconds: int64(opts.ResetCooldown / time.Second),
		flag:            opts.Flag,
		logger:          opts.Logger,
		now:             now,
	}
	// Register the source so SnapshotCircuitStates surfaces a gauge
	// even if the same flag is not yet wired into HTTPFetcher /
	// TransportCircuit. Idempotent — last writer wins.
	registerCircuitState(opts.Source, opts.Flag)
	return rw
}

// Observe folds one upstream outcome into the rate window:
//   - err == nil    → bumps the current bucket's ok counter.
//   - transport err → bumps the current bucket's err counter.
//   - other errors  → ignored (4xx / 5xx are application signals).
//
// After incrementing, the window's totals are recomputed and the trip
// rule (and reset rule, if AllowReset) is evaluated. Safe to call on a
// nil receiver — the call is a no-op (zero-flag breaker shape).
func (r *RateWindow) Observe(err error) {
	if r == nil || r.flag == nil {
		return
	}
	// Classify the error. Non-transport errors are ignored entirely:
	// they do NOT advance the ok counter either, since a 404 is not
	// a signal of upstream health — it's a "no data" signal.
	isErr := err != nil && isTransportOrDeadlineErr(err)
	if err != nil && !isErr {
		return
	}
	now := r.now().Unix()
	epoch := now / r.bucketSeconds
	idx := int(epoch % int64(len(r.buckets)))

	r.mu.Lock()
	if r.buckets[idx].epoch != epoch {
		// Rotate: the bucket's epoch has advanced, reset before
		// incrementing.
		r.buckets[idx] = rateBucket{epoch: epoch}
	}
	if isErr {
		r.buckets[idx].err++
	} else {
		r.buckets[idx].ok++
	}
	// Sum the live window. A bucket counts only if its epoch falls
	// within the last len(buckets) windows from now.
	minEpoch := epoch - int64(len(r.buckets)) + 1
	var totalOk, totalErr int64
	for _, b := range r.buckets {
		if b.epoch >= minEpoch && b.epoch <= epoch {
			totalOk += b.ok
			totalErr += b.err
		}
	}
	r.mu.Unlock()

	total := totalOk + totalErr
	var ratio float64
	if total > 0 {
		ratio = float64(totalErr) / float64(total)
	}

	// Trip rule. Two paths:
	//   - window has enough samples → fire on ratio > TripErrorRatio.
	//   - not enough samples yet    → fire on absolute err count.
	tripped := r.flag.Load()
	if !tripped {
		fire := false
		switch {
		case total >= int64(r.minSamples) && ratio > r.tripRatio:
			fire = true
		case r.minAbsErrors > 0 && totalErr >= int64(r.minAbsErrors):
			fire = true
		}
		if fire && r.flag.CompareAndSwap(false, true) {
			r.lastTripUnix.Store(now)
			recordCircuitTrip(r.source)
			lg := r.logger
			if lg == nil {
				lg = slog.Default()
			}
			lg.Warn("circuit tripped on rate-window error threshold",
				slog.String("source", r.source),
				slog.Int64("window_total", total),
				slog.Int64("window_err", totalErr),
				slog.Float64("error_ratio", ratio),
				slog.Float64("threshold", r.tripRatio),
			)
		}
		return
	}

	// Reset rule. Only consulted when AllowReset is true AND the
	// cooldown has elapsed AND the window holds at least MinSamples
	// observations. The "≥ MinSamples" guard prevents a freshly
	// emptied window (1 ok call after a long quiet period) from
	// immediately resetting a still-broken upstream.
	if r.allowReset {
		last := r.lastTripUnix.Load()
		if now-last >= r.cooldownSeconds && total >= int64(r.minSamples) && ratio <= r.resetRatio {
			if r.flag.CompareAndSwap(true, false) {
				lg := r.logger
				if lg == nil {
					lg = slog.Default()
				}
				lg.Warn("circuit auto-reset after rate-window recovery",
					slog.String("source", r.source),
					slog.Int64("window_total", total),
					slog.Int64("window_err", totalErr),
					slog.Float64("error_ratio", ratio),
					slog.Float64("reset_threshold", r.resetRatio),
				)
			}
		}
	}
}

// Tripped reports the current state of the underlying flag. Safe to
// call on a nil receiver (returns false).
func (r *RateWindow) Tripped() bool {
	if r == nil || r.flag == nil {
		return false
	}
	return r.flag.Load()
}

// Snapshot returns the current (ok, err, ratio) across the live window.
// Useful for tests and ad-hoc metrics. Safe to call on a nil receiver
// (returns zeros).
func (r *RateWindow) Snapshot() (totalOk, totalErr int64, ratio float64) {
	if r == nil {
		return 0, 0, 0
	}
	now := r.now().Unix()
	epoch := now / r.bucketSeconds
	minEpoch := epoch - int64(len(r.buckets)) + 1
	r.mu.Lock()
	for _, b := range r.buckets {
		if b.epoch >= minEpoch && b.epoch <= epoch {
			totalOk += b.ok
			totalErr += b.err
		}
	}
	r.mu.Unlock()
	total := totalOk + totalErr
	if total > 0 {
		ratio = float64(totalErr) / float64(total)
	}
	return totalOk, totalErr, ratio
}
