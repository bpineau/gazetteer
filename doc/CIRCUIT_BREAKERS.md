# Circuit breakers

The `helpers/circuit` package consolidates the per-Source breaker
pattern used by every HTTP-backed scraper / enricher. A breaker is a
single `*atomic.Bool` shared between the Source and any wrapper that
needs to ask "should I still be scheduling work against this
upstream?".

## Why

A maintenance run that walks thousands of listings against several
upstreams is dominated by tail latency from the slowest one. When one
upstream goes wedged — quota exhausted, anti-bot challenge, 5xx storm
— the rest of the run keeps burning O(retries × backoff × listings)
seconds against it before each call finally surfaces an error.

A breaker turns that into O(1) per call: once it trips, every further
Fetch on that Source short-circuits with `ErrCircuitOpen` (or a typed
`*CircuitTrippedError`) and the runner moves on.

## Three trip signals

| Signal                | What it catches                                  | Helper                           |
|-----------------------|--------------------------------------------------|----------------------------------|
| Transport / deadline  | network, TLS, HTTP/2 stream / GoAway, ctx errors | `HTTPFetcher` + `TransportCircuit`|
| 429 / quota header    | upstream is over its rate or per-key budget      | `HTTPFetcher` (built-in)         |
| Content empty streak  | HTTP 200 with paywall stub / antibot HTML        | `ContentStreakBreaker`           |

A single `*atomic.Bool` can be shared by any combination of breakers:
the caller checks `flag.Load()` once on the hot path and bails if any
breaker has tripped.

## The three APIs

### `HTTPFetcher` — wraps `httpx.Client.GetBytes`

Most Sources use the `GET → bytes` shape. `HTTPFetcher` is the
shortest path:

```go
var tripped atomic.Bool
fetcher := circuit.NewHTTPFetcher(httpClient, circuit.HTTPFetcherOptions{
    ErrPrefix:                     "bdnb",
    CircuitTripped:                &tripped,
    MaxConsecutiveTransportErrors: 5,
    Logger:                        slog.Default(),
})
body, err := fetcher.Fetch(ctx, "https://example.org/api/v1/x")
if tripped.Load() {
    // skip remaining work for this run
}
```

Trip triggers (any one flips the atomic):

- `MaxConsecutiveTransportErrors` consecutive transport / deadline
  failures with no intervening 2xx
- HTTP 429 (configurable via `MaxConsecutive429`; default 0 = trip on
  first 429)
- Response header `x-quota-remaining: 0` on any 2xx
- A non-nil `RateWindow` whose sliding-window error rate breaches its
  threshold

### `TransportCircuit` — for custom HTTP paths

When you talk to `httpx.Client` directly (POST bodies, custom
decoding, …) feed outcomes manually:

```go
var tripped atomic.Bool
cb := circuit.NewTransportCircuit("dvf", 5, &tripped, slog.Default())
cb.SetMax429(3) // optional

resp, err := httpClient.Do(req)
cb.Observe(err)
if cb.Tripped() {
    // skip remaining work
}
```

### `ContentStreakBreaker` — for "HTTP 200 with no signal"

Some upstreams answer 200 with a sign-in stub or an anti-bot
interstitial cached at the CDN. The transport breakers cannot see
this; `ContentStreakBreaker` can:

```go
var tripped atomic.Bool
cs := circuit.NewContentStreakBreaker("scraper", 10, &tripped, slog.Default())

body, err := fetcher.Fetch(ctx, url)
if err == nil {
    cs.Observe(body, func(b []byte) bool {
        return bytes.Contains(b, []byte("expected-marker"))
    })
}
if tripped.Load() {
    // 10 consecutive 200s with no marker — upstream is wedged
}
```

Sharing the same `*atomic.Bool` with a `TransportCircuit` /
`HTTPFetcher` makes a single `flag.Load()` cover every trip path.

## Cross-Source canonical sentinel

A Source whose breaker has tripped should return a typed
`*CircuitTrippedError`:

```go
var ErrCircuitTripped = gazetteer.NewCircuitTrippedError(Name)
```

Why a struct: per-Source identity match by pointer
(`errors.Is(err, dvf.ErrCircuitTripped)`) AND cross-Source match via
the type's `Is` method (`errors.Is(err, gazetteer.ErrSourceCircuitTripped)`).

That second form lets a downstream consumer aggregate "any Source
that tripped its breaker today" with one predicate.

## Process-wide metrics

Two snapshots:

- `circuit.SnapshotCircuitStates()` — current state per Source
  (Tripped bool). Emit one Prometheus gauge per Source.
- `circuit.SnapshotCircuitTripCounts()` — monotonic count of false→true
  flips per Source. Emit a counter; resets on process restart.

Both omit Sources with no registered breaker / zero count to keep
exports lean.

## Wiring it in a Source's `Options`

The canonical shape:

```go
type Options struct {
    HTTP            *httpx.Client
    CircuitTripped  *atomic.Bool  // caller-owned; allows sharing across instances
    // ...
}

func NewSource(opts Options) (*Source, error) {
    tc := circuit.NewTransportCircuit(
        Name,
        MaxConsecutiveTransportErrors,
        opts.CircuitTripped,
        opts.Logger,
    )
    tc.SetMax429(MaxConsecutive429)
    return &Source{
        opts: opts,
        api:  NewAPI(opts.HTTP, tc),
    }, nil
}

func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
    if s.opts.CircuitTripped != nil && s.opts.CircuitTripped.Load() {
        return nil, ErrCircuitTripped
    }
    // ...
}
```

The caller owns the `*atomic.Bool`. Long-running batch processes hold
one per (Source, run) so a fresh run starts fresh.

## Resetting

In production: never. A fresh process starts fresh atomics — that's
the whole semantic.

In tests:

```go
circuit.ResetCircuitTripCountersForTest()
circuit.ResetCircuitStateRegistryForTest()
```

Both panic-safe; both clear the process-wide registries.
