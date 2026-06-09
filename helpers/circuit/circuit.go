package circuit

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"sync"
	"sync/atomic"

	"golang.org/x/net/http2"

	"github.com/bpineau/gazetteer/helpers/httpx"
)

// circuitTripCounters tracks, per source, how many times the per-run
// circuit-breaker atomic has flipped from false to true in this
// process. Process-local, monotonic, never reset.
//
// Map values are *atomic.Int64 (heap-allocated) so concurrent fetchers
// across goroutines bump the same cell race-free. Key insertion uses
// LoadOrStore so the zero-allocation fast path stays hot once a source
// has been seen once.
var circuitTripCounters sync.Map // map[string]*atomic.Int64

// recordCircuitTrip increments the process-wide counter for source.
// Called exactly once per (run × source) — at the moment a circuit
// flips from false to true. A run that re-trips the same source by
// resetting the atomic (not currently a code path) would double-count,
// but the atomic is never reset within a process today.
func recordCircuitTrip(source string) {
	if source == "" {
		source = "common"
	}
	c, _ := circuitTripCounters.LoadOrStore(source, &atomic.Int64{})
	c.(*atomic.Int64).Add(1)
}

// tripCircuit flips flag from false to true, bumping the process-wide
// trip counter on the transition only. Returns true exactly once per
// flag lifetime — the false→true transition — so callers can attach
// one-shot side effects (e.g. the warn line) without re-checking the
// CAS. A nil flag is a no-op that returns false.
func tripCircuit(flag *atomic.Bool, source string) bool {
	if flag == nil || !flag.CompareAndSwap(false, true) {
		return false
	}
	recordCircuitTrip(source)
	return true
}

// tripAndWarn is tripCircuit plus the single Warn line every streak-style
// breaker in this package emits at the moment its threshold is crossed.
// The message and attrs are passed through verbatim (consumers pin the
// log shapes operationally — do not reword); `source` is always emitted
// as the first attr. A nil lg falls back to slog.Default(). Idempotent:
// an already-tripped flag yields neither a counter bump nor a log line.
func tripAndWarn(flag *atomic.Bool, source, msg string, lg *slog.Logger, attrs ...slog.Attr) {
	if !tripCircuit(flag, source) {
		return
	}
	if lg == nil {
		lg = slog.Default()
	}
	all := make([]slog.Attr, 0, len(attrs)+1)
	all = append(all, slog.String("source", source))
	all = append(all, attrs...)
	lg.LogAttrs(context.Background(), slog.LevelWarn, msg, all...)
}

// SnapshotCircuitTripCounts returns a (source → trip_count) snapshot
// sorted alphabetically by source. Designed for Prometheus / metrics
// scrapers: sources with a zero count are omitted because they have
// never tripped, so emitting them as 0 would add noise without signal.
func SnapshotCircuitTripCounts() []CircuitTripCount {
	var out []CircuitTripCount
	circuitTripCounters.Range(func(k, v any) bool {
		n := v.(*atomic.Int64).Load()
		if n > 0 {
			out = append(out, CircuitTripCount{Source: k.(string), Count: n})
		}
		return true
	})
	slices.SortFunc(out, func(a, b CircuitTripCount) int { return cmp.Compare(a.Source, b.Source) })
	return out
}

// CircuitTripCount is one (source, count) sample emitted by
// SnapshotCircuitTripCounts.
type CircuitTripCount struct {
	Source string
	Count  int64
}

// ResetCircuitTripCountersForTest wipes the process-local counter map.
// Test-only — production code never resets the counters (the metric is
// monotonic per process lifetime).
func ResetCircuitTripCountersForTest() {
	circuitTripCounters.Range(func(k, _ any) bool {
		circuitTripCounters.Delete(k)
		return true
	})
}

// circuitStateRegistry tracks, per source, the live *atomic.Bool that
// HTTPFetcher checks on every Fetch. Snapshot reads each pointer to
// surface "is the circuit currently tripped?" — the gauge counterpart
// to the monotonic trip counter above.
//
// Registration happens in NewHTTPFetcher when a non-nil CircuitTripped
// (or its legacy alias QuotaTripped) is supplied. The registry is
// process-local.
//
// Values are the same *atomic.Bool pointers the calling code shares
// with HTTPFetcherOptions, so a Load() reflects the live state without
// copy. Re-registration of the same source replaces the prior pointer
// (last NewHTTPFetcher wins).
var circuitStateRegistry sync.Map // map[string]*atomic.Bool

// registerCircuitState wires source → flag into circuitStateRegistry.
// Called by NewHTTPFetcher when the caller passes a non-nil circuit
// pointer. An empty source key is mapped to "common" so the snapshot
// label is never blank.
func registerCircuitState(source string, flag *atomic.Bool) {
	if flag == nil {
		return
	}
	if source == "" {
		source = "common"
	}
	circuitStateRegistry.Store(source, flag)
}

// CircuitState is one (source, tripped) sample returned by
// SnapshotCircuitStates.
type CircuitState struct {
	Source  string
	Tripped bool
}

// SnapshotCircuitStates returns a (source → tripped) snapshot sorted
// alphabetically by source. Designed for Prometheus / metrics
// scrapers: emit one gauge per source (0 when clean, 1 when tripped).
// Sources with no registered HTTPFetcher are omitted.
func SnapshotCircuitStates() []CircuitState {
	var out []CircuitState
	circuitStateRegistry.Range(func(k, v any) bool {
		flag, ok := v.(*atomic.Bool)
		if !ok || flag == nil {
			return true
		}
		out = append(out, CircuitState{Source: k.(string), Tripped: flag.Load()})
		return true
	})
	slices.SortFunc(out, func(a, b CircuitState) int { return cmp.Compare(a.Source, b.Source) })
	return out
}

// ResetCircuitStateRegistryForTest wipes the process-local state map.
// Test-only — production never clears the registry (the pointers are
// owned by long-lived caller-owned Deps).
func ResetCircuitStateRegistryForTest() {
	circuitStateRegistry.Range(func(k, _ any) bool {
		circuitStateRegistry.Delete(k)
		return true
	})
}

// ErrCircuitOpen is returned by HTTPFetcher.Fetch when the shared
// circuit-breaker atomic has been flipped before the call. Callers
// that paginate or fan-out across many URLs should treat this as the
// signal to bail the current loop without retry — the per-source flag
// stays tripped for the rest of the process, so a retry would only
// surface the same error after the next 5×exp-backoff retry tax.
//
// Wrapped by HTTPFetcher with the per-source ErrPrefix + URL so the
// log line still localises the failure; callers can match via
// errors.Is(err, circuit.ErrCircuitOpen).
var ErrCircuitOpen = errors.New("circuit: open (skipping further calls)")

// Fetcher abstracts "fetch the body for a URL" so a caller can be
// fully unit-tested without an httpx.Client. Production wiring uses
// HTTPFetcher (a thin wrapper over httpx.Client.GetBytes); tests inject
// FuncFetcher.
type Fetcher interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
}

// FuncFetcher adapts a plain function to the Fetcher interface. Tests
// use it to return canned bodies without spawning an httptest.Server.
type FuncFetcher func(ctx context.Context, url string) ([]byte, error)

// Fetch implements Fetcher.
func (f FuncFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	return f(ctx, url)
}

// HTTPFetcherOptions configures an HTTPFetcher. Every field is
// optional; the zero value yields a fetcher that issues a plain GET
// with no extra headers.
type HTTPFetcherOptions struct {
	// ErrPrefix is the leading token of the wrapped error string, used
	// to disambiguate which source produced the failure when a stack
	// of fetches funnels into one log line. Defaults to "common".
	ErrPrefix string

	// Headers, when non-nil, is applied verbatim to every request.
	// Callers should pass a header set with Accept (and any per-source
	// hint such as Sec-Fetch-Site, X-Requested-With, Referer) already
	// populated. Headers are NOT cloned per-call; callers must not
	// mutate the value after constructing the fetcher.
	Headers http.Header

	// CircuitTripped, when non-nil, is set to true the first time the
	// upstream signals it is unfit for further work for the rest of
	// this run. Two kinds of signals trip it today:
	//
	//   - quota exhaustion : HTTP 429 (after retries) or response header
	//     `x-quota-remaining: 0` observed on a 2xx.
	//
	//   - sustained transport failures : MaxConsecutiveTransportErrors
	//     successive transport / context-deadline failures with no
	//     intervening 2xx.
	//
	// Callers check this flag before scheduling further fetches,
	// allowing the rest of a maintenance run to skip a dead upstream.
	// The flag is process-local: a new run starts fresh.
	CircuitTripped *atomic.Bool

	// QuotaTripped is the legacy name for CircuitTripped. When both
	// are non-nil CircuitTripped wins; when only QuotaTripped is set
	// we use it. Kept so existing call sites that wired the old name
	// don't need to change at the same time as this rename. New call
	// sites should use CircuitTripped.
	QuotaTripped *atomic.Bool

	// MaxConsecutiveTransportErrors, when > 0, enables the consecutive-
	// transport-error circuit breaker. After this many successive
	// transport / deadline failures with no 2xx in between, CircuitTripped
	// is flipped (and a Warn line is emitted via Logger when non-nil).
	// A zero value disables the consecutive-error breaker; the quota
	// signals above still trip the flag.
	MaxConsecutiveTransportErrors int

	// MaxConsecutive429, when > 0, requires N successive HTTP 429
	// responses (after httpx-level retries) with no intervening 2xx
	// before flipping CircuitTripped. The default zero preserves the
	// historic "trip on the first 429" semantic — useful for upstreams
	// whose 429 is a hard quota signal (BDNB, ADEME). Set to N>0 for
	// upstreams that 429 sporadically under load without exhausting a
	// quota; the breaker only fires when the streak is sustained.
	//
	// The `x-quota-remaining: 0` header signal is independent: it
	// always trips on first observation regardless of MaxConsecutive429.
	MaxConsecutive429 int

	// Logger is used to surface a single Warn line at the moment the
	// consecutive-transport-error threshold is reached. Optional: when
	// nil, slog.Default() is used.
	Logger *slog.Logger

	// RateWindow, when non-nil, enables a complementary sliding-window
	// rate breaker fed alongside the consecutive-streak counters. Every
	// Fetch outcome is forwarded to RateWindow.Observe — a 2xx feeds the
	// ok side, a transport / deadline failure feeds the err side, and
	// other errors are ignored. The rate-window catches "slow burn"
	// failure modes (30-60 % of requests failing for several minutes
	// in a row) that the consecutive-streak breakers miss because each
	// failure is followed by enough 2xx to keep the streak pinned at
	// zero.
	//
	// The RateWindow shares the same CircuitTripped / QuotaTripped
	// atomic — both breaker paths flip the same flag, the caller
	// checks Load() once on the hot path.
	RateWindow *RateWindow
}

// HTTPFetcher is the production Fetcher implementation. It forwards to
// httpx.Client.GetBytes with the configured headers. The Client
// handles retries, rate-limits and caching.
//
// The Client field is mandatory. Construct via NewHTTPFetcher.
type HTTPFetcher struct {
	Client  *httpx.Client
	Options HTTPFetcherOptions

	// consecutiveTransportErrors tracks the run of successive transport
	// failures since the last 2xx. Reset to 0 on each successful Fetch;
	// reaching Options.MaxConsecutiveTransportErrors trips the circuit.
	//
	// NOTE: TransportCircuit implements a sibling pair of streak
	// counters (consec / consec429) for callers that bypass Fetch.
	// They share tripAndWarn (same trip + log shape) but are NOT one
	// machine: this fetcher additionally handles the quota header and
	// the first-429-trips-immediately default, classifies a single
	// error into both streaks independently, and re-reads thresholds
	// from Options on every call — whereas TransportCircuit picks one
	// streak per error (transport wins), ignores 429s by default, and
	// is a full no-op without a flag. Unifying them would silently
	// change one side's semantics; keep edits in sync by hand.
	consecutiveTransportErrors atomic.Int32

	// consecutive429 tracks the run of successive HTTP 429 responses
	// since the last 2xx. Reset to 0 on each successful Fetch; reaching
	// Options.MaxConsecutive429 trips the circuit. Only consulted when
	// Options.MaxConsecutive429 > 0 — at zero, the first 429 trips
	// immediately via the quota-exhaustion path (back-compat).
	consecutive429 atomic.Int32
}

// NewHTTPFetcher returns an HTTPFetcher bound to c with the provided
// options. The Headers map is retained as-is — callers should treat
// it as immutable after this call.
//
// When opts.CircuitTripped (or its legacy alias QuotaTripped) is
// non-nil, the pointer is registered in the process-wide state map so
// SnapshotCircuitStates can read its current value as a gauge.
func NewHTTPFetcher(c *httpx.Client, opts HTTPFetcherOptions) *HTTPFetcher {
	if opts.ErrPrefix == "" {
		opts.ErrPrefix = "common"
	}
	h := &HTTPFetcher{Client: c, Options: opts}
	if flag := h.circuitFlag(); flag != nil {
		registerCircuitState(opts.ErrPrefix, flag)
	}
	return h
}

// Fetch implements Fetcher. Returns the body verbatim on 2xx, and a
// wrapped error on transport / non-2xx.
func (h *HTTPFetcher) Fetch(ctx context.Context, url string) ([]byte, error) {
	if h == nil {
		return nil, errors.New("circuit: nil HTTPFetcher")
	}
	if h.Client == nil {
		prefix := h.Options.ErrPrefix
		if prefix == "" {
			prefix = "common"
		}
		return nil, errors.New(prefix + ": nil HTTPFetcher")
	}
	// Pre-flight breaker check (defence layer 2 of 3 — see also the
	// per-enricher Enrich() guard one level up + the runner-level
	// consecutive-failure disable one level above that). Once the
	// shared atomic has been flipped, every further Fetch on this
	// source would burn a 5×exp-backoff retry sequence against a
	// wedged upstream before surfacing a 429 to the caller. Refusing
	// here turns the failure-mode into a same-loop no-op: callers
	// that keep iterating (multi-page paginators, fallback ladders
	// that don't notice the trip immediately) bail in O(1) instead
	// of O(timeout × retries × pages).
	if flag := h.circuitFlag(); flag != nil && flag.Load() {
		return nil, fmt.Errorf("%s fetch %s: %w", h.Options.ErrPrefix, url, ErrCircuitOpen)
	}
	body, resp, err := h.Client.GetBytes(ctx, url, h.Options.Headers)
	circuit := h.circuitFlag()
	// Detect upstream signals once. is429 is true on any 429 (including
	// retry-exhausted 429s wrapped in *ErrTooManyRetries). quotaHeader is
	// true when the upstream returned 2xx with `x-quota-remaining: 0`.
	is429 := errIs429(err)
	quotaHeader := respHasQuotaRemainingZero(resp)
	// On any 2xx (err == nil) reset both consecutive streaks. We do this
	// up-front so the rest of the function only deals with the failure
	// paths.
	if err == nil {
		h.consecutiveTransportErrors.Store(0)
		h.consecutive429.Store(0)
	}
	if circuit != nil {
		// Quota-exhaustion is a process-wide signal. The first time we
		// see HTTP 429 (with MaxConsecutive429 == 0) or
		// `x-quota-remaining: 0` we flip the atomic so the calling
		// code's next ShouldRun short-circuits without burning another
		// retry-backoff window. We never reset the flag — a fresh run
		// starts a fresh atomic. The process-wide trip counter is
		// bumped on the false→true transition only so metrics count
		// flips, not steady-state "still tripped" calls.
		switch {
		case quotaHeader:
			tripCircuit(circuit, h.Options.ErrPrefix)
		case is429 && h.Options.MaxConsecutive429 <= 0:
			// Historic semantic: first 429 trips immediately.
			tripCircuit(circuit, h.Options.ErrPrefix)
		case is429 && h.Options.MaxConsecutive429 > 0:
			// Streak-based semantic: only trip after N consecutive
			// 429s with no intervening 2xx. Sporadic 429s reset on
			// the next 2xx (see the err == nil branch above).
			n := h.consecutive429.Add(1)
			if int(n) >= h.Options.MaxConsecutive429 {
				tripAndWarn(circuit, h.Options.ErrPrefix,
					"circuit tripped on consecutive 429 responses", h.Options.Logger,
					slog.Int("consecutive_429", int(n)),
					slog.Int("threshold", h.Options.MaxConsecutive429),
				)
			}
		}
	}
	// Consecutive transport-error circuit breaker. Enabled when
	// MaxConsecutiveTransportErrors > 0.
	if h.Options.MaxConsecutiveTransportErrors > 0 {
		if err != nil && isTransportOrDeadlineErr(err) {
			n := h.consecutiveTransportErrors.Add(1)
			if int(n) >= h.Options.MaxConsecutiveTransportErrors {
				tripAndWarn(circuit, h.Options.ErrPrefix,
					"circuit tripped on consecutive transport errors", h.Options.Logger,
					slog.Int("consecutive_errors", int(n)),
					slog.Int("threshold", h.Options.MaxConsecutiveTransportErrors),
				)
			}
		}
	}
	// Sliding-window rate breaker. Fed every Fetch outcome — the
	// RateWindow internally ignores non-transport errors so we forward
	// err verbatim. The 2xx case (err == nil) feeds the ok counter so
	// the window's reset rule can fire after a quiet run.
	if h.Options.RateWindow != nil {
		h.Options.RateWindow.Observe(err)
	}
	if err != nil {
		return nil, fmt.Errorf("%s fetch %s: %w", h.Options.ErrPrefix, url, err)
	}
	return body, nil
}

// circuitFlag returns the active circuit-breaker atomic, honouring the
// CircuitTripped → QuotaTripped fallback for back-compat.
func (h *HTTPFetcher) circuitFlag() *atomic.Bool {
	if h.Options.CircuitTripped != nil {
		return h.Options.CircuitTripped
	}
	return h.Options.QuotaTripped
}

// isTransportOrDeadlineErr returns true when err is (or wraps) a
// transport failure or a context deadline / cancellation.
//
// Covers both the typed wrapper that httpx.Client surfaces
// (*httpx.ErrTransport) and the raw stdlib variants callers that
// bypass httpx.GetBytes / GetJSON observe (e.g. raw http.Client.Do):
// *url.Error, *net.OpError, io.EOF / io.ErrUnexpectedEOF, ctx
// cancellation / deadline, and the HTTP/2 connection-level errors
// (http2.StreamError, *http2.GoAwayError, http2.ConnectionError)
// that the stdlib http2 layer surfaces verbatim when an upstream
// crashes mid-stream — georisques.gouv.fr's INTERNAL_ERROR episodes
// are this exact shape, and without explicit recognition the circuit
// breaker fails to trip because http2.StreamError is neither a
// *net.OpError nor a net.Error.
func isTransportOrDeadlineErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	if _, ok := errors.AsType[*httpx.ErrTransport](err); ok {
		return true
	}
	if _, ok := errors.AsType[*net.OpError](err); ok {
		return true
	}
	// HTTP/2 stream / connection failures from golang.org/x/net/http2.
	// StreamError is a value type (no pointer), GoAwayError /
	// ConnectionError are pointer / value respectively; cover all three
	// shapes so the breaker sees the upstream collapse regardless of
	// which http2 internal frame triggered it.
	if _, ok := errors.AsType[http2.StreamError](err); ok {
		return true
	}
	if _, ok := errors.AsType[*http2.GoAwayError](err); ok {
		return true
	}
	if _, ok := errors.AsType[http2.ConnectionError](err); ok {
		return true
	}
	// *url.Error wraps a transport-level error. Only count it when the
	// underlying cause is itself transport-shaped — a 4xx that bubbles
	// up here would also be wrapped in *url.Error and we must not flip
	// the breaker on application errors.
	var uerr *url.Error
	if errors.As(err, &uerr) && uerr.Err != nil {
		return isTransportOrDeadlineErr(uerr.Err)
	}
	return false
}

// TransportCircuit is a thin helper for callers that talk to an
// upstream through httpx.Client directly (POST bodies, custom
// decoding, ad-hoc 404 handling) rather than via HTTPFetcher's GET-only
// path. Construct one via NewTransportCircuit; each call to Observe
// folds the (err) outcome into the rolling counter, flipping the
// shared atomic when the threshold is hit.
//
// The helper registers itself in the process-wide circuit-state map
// the same way HTTPFetcher does, so SnapshotCircuitStates surfaces a
// gauge for these callers too.
//
// NOTE: the consec / consec429 pair mirrors HTTPFetcher's
// consecutiveTransportErrors / consecutive429 streaks (same thresholds-
// trip-warn shape, shared via tripAndWarn) but the gating semantics
// differ deliberately — see the cross-reference comment on
// HTTPFetcher.consecutiveTransportErrors before attempting to merge.
type TransportCircuit struct {
	source    string
	threshold int
	logger    *slog.Logger
	flag      *atomic.Bool
	consec    atomic.Int32

	// max429, when > 0, enables a parallel "consecutive 429 responses"
	// streak counter. Set via SetMax429. A zero value disables the 429
	// tracker entirely — callers that talk to a quota-aware upstream
	// (BDNB header) should NOT use this counter, the HTTPFetcher flow
	// is the right one for them.
	max429    int
	consec429 atomic.Int32
}

// NewTransportCircuit returns a TransportCircuit bound to flag. The
// source label is used for both the metrics gauge and the trip
// warning. A nil flag yields a no-op tracker (every Observe / Tripped
// returns the zero value); a non-positive threshold disables the
// counter (only an explicit Trip() call could flip the flag — not
// implemented today).
//
// When flag is non-nil, the pointer is registered in the process-wide
// state map. Re-registering the same source replaces the prior pointer.
func NewTransportCircuit(source string, threshold int, flag *atomic.Bool, logger *slog.Logger) *TransportCircuit {
	if source == "" {
		source = "common"
	}
	tc := &TransportCircuit{
		source:    source,
		threshold: threshold,
		logger:    logger,
		flag:      flag,
	}
	if flag != nil {
		registerCircuitState(source, flag)
	}
	return tc
}

// Tripped reports whether the underlying flag has been flipped. Safe
// to call on a nil receiver.
func (t *TransportCircuit) Tripped() bool {
	if t == nil || t.flag == nil {
		return false
	}
	return t.flag.Load()
}

// SetMax429 enables (or disables when n<=0) the consecutive-429
// breaker on this circuit. Safe to call on a nil receiver. The
// counter is independent from the transport-error counter: a 429
// run does NOT advance the transport streak and vice versa, but
// any single 2xx (Observe(nil)) resets BOTH streaks.
func (t *TransportCircuit) SetMax429(n int) {
	if t == nil {
		return
	}
	t.max429 = n
}

// Observe folds the outcome of one upstream call into the counter:
//   - err == nil resets the transport streak AND the 429 streak.
//   - isTransportOrDeadlineErr(err) ticks the transport streak; on
//     threshold the atomic is flipped (idempotent — only the first
//     transition is counted by the metrics).
//   - an err wrapping *httpx.ErrHTTP{Status:429} ticks the 429 streak
//     (when SetMax429(n>0) is enabled); on threshold the same atomic
//     is flipped.
//   - other errors are ignored (e.g. non-429 4xx, JSON decode failure).
//
// Safe to call on a nil receiver — the call is a no-op.
func (t *TransportCircuit) Observe(err error) {
	if t == nil || t.flag == nil {
		return
	}
	if err == nil {
		t.consec.Store(0)
		t.consec429.Store(0)
		return
	}
	switch {
	case isTransportOrDeadlineErr(err):
		if t.threshold <= 0 {
			return
		}
		n := t.consec.Add(1)
		if int(n) >= t.threshold {
			tripAndWarn(t.flag, t.source,
				"circuit tripped on consecutive transport errors", t.logger,
				slog.Int("consecutive_errors", int(n)),
				slog.Int("threshold", t.threshold),
			)
		}
	case errIs429(err):
		if t.max429 <= 0 {
			return
		}
		n := t.consec429.Add(1)
		if int(n) >= t.max429 {
			tripAndWarn(t.flag, t.source,
				"circuit tripped on consecutive 429 responses", t.logger,
				slog.Int("consecutive_429", int(n)),
				slog.Int("threshold", t.max429),
			)
		}
	}
}

// errIs429 reports whether err is (or wraps) an *httpx.ErrHTTP with
// status 429. The httpx retry layer surfaces the final 429 either
// directly as *ErrHTTP or wrapped inside *ErrTooManyRetries — both
// shapes are covered by errors.AsType's tree walk.
func errIs429(err error) bool {
	if err == nil {
		return false
	}
	herr, ok := errors.AsType[*httpx.ErrHTTP](err)
	return ok && herr.Status == http.StatusTooManyRequests
}

// respHasQuotaRemainingZero reports whether the upstream returned 2xx
// with `x-quota-remaining: 0` — the budget is gone, the next call
// would 429 anyway. BDNB / ADEME-style quota-aware APIs use this.
func respHasQuotaRemainingZero(resp *httpx.Response) bool {
	if resp == nil || resp.Header == nil {
		return false
	}
	return resp.Header.Get("x-quota-remaining") == "0"
}
