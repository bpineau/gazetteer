# Concepts

The gazetteer library is built around a small vocabulary. Understanding
the seven types below covers ~95 % of day-to-day use.

## The mental model

```
                  (1)             (2)              (3)
   free-text  ─────────►  Listing  ──────► Source ──────► Result(typed Data)
   "1 rue X"  Normalizer            Query                  + Status
                                                           + Evidence (opt.)

                                  ┌── Source A ──► Result A ─┐
   Client.Collect(ctx, listing) ──┤── Source B ──► Result B ─┤── Dossier
                                  └── Source N ──► Result N ─┘
```

1. `Normalizer.Normalize` canonicalises free text into a `Listing`.
2. Each `Source.Query` produces a typed payload for the same listing.
3. The framework wraps each (payload, error) into a `Result` envelope
   and collects them into a `Dossier`.

`appraisal.PricePerM2`, `appraisal.RentValue` and `appraisal.HazardProfile`
then run secondary synthesis over the Dossier — see the appraisal
godoc.

## Types

### `Listing` — the universal input

```go
type Listing struct {
    Address      string
    City, Zip    string
    INSEE        string
    Lat, Lon     *float64
    PropertyType PropertyType
    SurfaceM2    *float64
    Rooms        *int
    BuildYear    *int
    AsOf         time.Time
}
```

Most numeric fields are pointers so absent is unambiguous and zero is a
legal value. Each `Source` decides whether the fields it needs are
present and returns `gazetteer.ErrInsufficientInputs` if not.

`PropertyType` is the coarse, source-agnostic classification used to
gate per-Source eligibility (DVF skips parking lots, BienIci skips
commercial, etc.).

### `Source` — a named, versioned data origin

```go
type Source interface {
    Name() string
    Version() int
    Query(ctx context.Context, listing Listing) (any, error)
}
```

- `Name()` is the registry key and the `Dossier.Results` map key. By
  convention each package also exports it as `const Name`.
- `Version()` is a monotonic integer bumped when the Source's internal
  logic changes; stateful callers gate cache invalidation on it.
- `Query` returns a typed payload; the framework wraps the (payload,
  error) pair into a `Result`.

A Source MAY implement optional interfaces — see [plugins.md](plugins.md).

### `Result` — the framework envelope

```go
type Result struct {
    Name      string
    Version   int
    Status    Status
    InputHash string
    FetchedAt time.Time
    Err       error
    Data      any   // typed payload
    Evidence  any   // optional reproducibility sidecar
}
```

`Status` classifies the outcome (see below). `Data` is a pointer to a
package-defined typed struct (`*dvf.Result`, `*osm.Result`, …).
`Evidence`, when populated, captures input fingerprint, ladder tier
used, resolver provenance — anything that helps a downstream consumer
reproduce the answer.

### `Status` — outcome taxonomy

| Status              | Meaning                                       |
|---------------------|-----------------------------------------------|
| `ok`                | typed `Data` populated                        |
| `ok_empty`          | ran successfully, but `Data.IsEmpty()` is true|
| `skipped_prereq`    | inputs missing or property type unsupported   |
| `failed_transient`  | network / 5xx / generic error                 |
| `failed_antibot`    | anti-bot interstitial detected                |
| `failed_outdated`   | parser cannot read the response — operator fix|
| `failed_permanent`  | upstream broken in a way the source can't fix |

The Client translates each Source error to one of these via the sentinel
table in `gazetteer/errors.go`.

### `Dossier` — the aggregated output

```go
type Dossier struct {
    Listing    Listing
    Results    map[string]Result // keyed by Source.Name()
    StartedAt, FinishedAt time.Time
}
```

`Get[T]` is the canonical accessor:

```go
if r, ok := gazetteer.Get[*dvf.Result](dossier, dvf.Name); ok {
    fmt.Println(r.SampleSize, r.Evidence.LevelUsed)
}
```

`Dossier` is JSON-roundtrip-safe: its `MarshalJSON` / `UnmarshalJSON`
use the registry (`gazetteer.Register`) to reconstitute concrete typed
payloads from wire bytes.

### `Builder` / `Client`

`Builder` is a configuration step:

```go
b := gazetteer.NewBuilder().
    WithHTTPClient(hc).
    WithLogger(lg).
    WithNormalizer(banx-normalizer).
    With(dvfSource).
    With(osmSource)
client, err := b.Build()
```

`Client` is the resulting immutable object:

```go
dossier := client.Collect(ctx, listing)
```

`Collect` runs every Source in parallel (bounded by
`WithMaxConcurrency` if set). Per-Source errors land on the `Result`
envelope; `Collect` itself never returns an error.

### `Normalizer`

```go
type Normalizer interface {
    Normalize(ctx context.Context, addr string) (Listing, error)
}
```

`Client.Normalize` delegates to the configured Normalizer. The
`factory.NewDefault` wiring installs `gazetteer.BANNormalizer` (BAN
forward geocoder + `helpers/communes` table) by default.

### Cache (`kvcache.Cache`)

A pluggable persistent key/value cache that the Sources needing
cross-run memo can consume:

- BAN geocode cache (`helpers/banx.CachedGeocoder`)
- DVF cadastral section catalog (`sources/dvf.SectionDiscoverer`)
- Any out-of-tree Source's per-zone lookups

See [caching.md](caching.md) for the contract and conformance suite.

## Concurrency model

```
Client.Collect(ctx, listing)
    │
    ├─ go runOne(ctx, srcA, listing)  ──► Result A ──┐
    ├─ go runOne(ctx, srcB, listing)  ──► Result B ──┤
    └─ go runOne(ctx, srcN, listing)  ──► Result N ──┘
                                                     │
                              gathered into Dossier ◄┘
```

- `Collect` propagates the configured HTTP client, logger and
  debug-dump flag via the context-key helpers (`gazetteer.WithHTTPClient`,
  `WithLogger`, `WithDebugDump`).
- Sources read these via the `*From(ctx)` helpers — see
  `gazetteer/context.go`.
- A Source MAY override the HTTP client per-instance via its own
  `Options.HTTPClient` field.
- Sources MUST honour `ctx.Done()`.

## Error semantics

Errors are sentinels (`gazetteer.ErrInsufficientInputs`,
`ErrAntiBot`, `ErrUpstreamSchemaChanged`, …) declared in
`gazetteer/errors.go`. Sources wrap them with `fmt.Errorf("%w: ...")` so
callers identify them via `errors.Is`.

The framework's `classifyErr` translates each sentinel into a `Status`.
A Source that needs a fresh failure-mode classification adds a sentinel
in `errors.go` and a switch arm in `classifyErr`.

### Circuit-tripped errors

When a Source's circuit breaker opens for the rest of a run, the Source
returns a `*CircuitTrippedError`. The cross-source check:

```go
errors.Is(err, gazetteer.ErrSourceCircuitTripped)
```

matches every per-Source circuit error. The per-Source-specific match:

```go
errors.Is(err, dvf.ErrCircuitTripped)
```

matches by pointer identity against the Source's own singleton. See
[circuit_breakers.md](circuit_breakers.md).

## Quick example

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/bpineau/gazetteer/factory"
    "github.com/bpineau/gazetteer/gazetteer"
    "github.com/bpineau/gazetteer/sources/dvf"
)

func main() {
    ctx := context.Background()
    client, err := factory.NewDefault(ctx)
    if err != nil { log.Fatal(err) }

    listing, err := client.Normalize(ctx, "1 rue de Rivoli, 75001 Paris")
    if err != nil { log.Fatal(err) }

    dossier := client.Collect(ctx, listing)

    if r, ok := gazetteer.Get[*dvf.Result](dossier, dvf.Name); ok {
        fmt.Printf("DVF: sample_size=%d, level=%s\n",
            r.SampleSize, r.Evidence.LevelUsed)
    }
}
```
