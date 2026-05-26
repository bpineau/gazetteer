# circuit — per-source circuit breaker for HTTP scrapers

A small Go library that consolidates the per-source circuit-breaker
pattern every HTTP-backed scraper / enricher needs:

- **Trip on N consecutive transport errors** (with configurable
  threshold).
- **Trip on a sliding-window error rate** — catches "slow burn"
  failure modes the consecutive-streak counter misses (5-min window
  by default, > 50 % errors fires). See `RateWindow`.
- **Trip on observed quota exhaustion**: HTTP 429 (after retries) or
  the response header `x-quota-remaining: 0`.
- **Expose process-local gauge + monotonic counter** via snapshot
  helpers so a metrics handler can render them as Prometheus / OTLP
  series.

The breaker is a `*atomic.Bool` that callers own and share with the
package; the `Load()` check is a single atomic on the hot path.

## Two APIs

| When you have… | Use |
|---|---|
| A plain GET-shaped scraper hitting one URL pattern | `HTTPFetcher` + `Fetcher` interface |
| POST bodies, custom decoding, or you call `httpx.Client.Do` directly | `TransportCircuit` (manual `Observe(err)`) |

Both register their flag with the same process-wide state map; the
`SnapshotCircuitStates` / `SnapshotCircuitTripCounts` helpers see the
union.

## Example — HTTPFetcher

```go
package myscraper

import (
    "context"
    "log/slog"
    "sync/atomic"

    "myrepo/pkg/circuit"
    "myrepo/pkg/httpx"
)

func Scrape(ctx context.Context, httpClient *httpx.Client) {
    var tripped atomic.Bool

    fetcher := circuit.NewHTTPFetcher(httpClient, circuit.HTTPFetcherOptions{
        ErrPrefix:                     "bdnb",
        CircuitTripped:                &tripped,
        MaxConsecutiveTransportErrors: 5,
        Logger:                        slog.Default(),
    })

    for _, url := range urlsToFetch {
        if tripped.Load() {
            slog.Default().Warn("circuit open, skipping", "url", url)
            return
        }
        body, err := fetcher.Fetch(ctx, url)
        _ = body
        _ = err
    }
}
```

## Example — TransportCircuit (manual)

```go
var tripped atomic.Bool
cb := circuit.NewTransportCircuit("dvf", 5, &tripped, slog.Default())

resp, err := httpClient.Do(req) // raw POST or whatever
cb.Observe(err)

if cb.Tripped() {
    // skip remaining work
}
```

## Metrics

```go
for _, s := range circuit.SnapshotCircuitStates() {
    // render `myapp_circuit_state{source="<s.Source>"} <0|1>`
}
for _, c := range circuit.SnapshotCircuitTripCounts() {
    // render `myapp_circuit_tripped_total{source="<c.Source>"} <c.Count>`
}
```

The package itself emits no metrics; it just exposes the structured
data so the caller can choose the format and naming scheme.

## Sliding-window rate breaker

`RateWindow` is a complement to the consecutive-streak counters: it
catches failure modes where 30-60 % of requests fail for several
minutes, interleaved with enough 2xx to keep the streak counter
pinned at zero.

```go
var tripped atomic.Bool
rw := circuit.NewRateWindow(circuit.RateWindowOptions{
    Source:         "georisques",
    Window:         5 * time.Minute, // sliding span
    BucketCount:    10,              // 30 s per bucket
    TripErrorRatio: 0.50,            // > 50 % errors → trip
    MinSamples:     20,              // need ≥ 20 reqs for ratio rule
    TripMinAbsoluteErrors: 10,       // absolute fallback below MinSamples
    ResetErrorRatio: 0.30,           // ≤ 30 % errors → auto-reset
    AllowReset:     true,            // opt-in; off by default
    Flag:           &tripped,
})
fetcher := circuit.NewHTTPFetcher(httpClient, circuit.HTTPFetcherOptions{
    ErrPrefix:      "georisques",
    CircuitTripped: &tripped,
    RateWindow:     rw,
})
```

The same `*atomic.Bool` is flipped by both the streak counter and the
rate-window — callers check `Load()` once on the hot path. Auto-reset
is opt-in because quota-style breakers (BDNB, ADEME) must stay tripped
for the rest of the run; transient-transport breakers (Georisques
HTTP/2, Castorus anti-bot) want auto-recovery.

## Quota signals

`HTTPFetcher` flips the breaker on either of these upstream signals:

| Signal | Detection |
|---|---|
| HTTP 429 | `errors.AsType[*httpx.ErrHTTP](err).Status == 429` |
| `x-quota-remaining: 0` | Header on a 2xx response |

Either signal flips the flag at most once per process lifetime
(idempotent on the false→true transition).

## Dependencies

- `myrepo/pkg/httpx` — request types and error sentinels.

No other internal dependency.
