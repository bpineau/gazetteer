# Writing a Source plugin

A Source plugin is any Go package outside the `gazetteer` tree that
implements `gazetteer.Source`. The framework has no compile-time
knowledge of which Sources are wired — your plugin is on the same
footing as the shipped Sources under `sources/`.

## Minimum viable plugin

```go
package myplugin

import (
    "context"

    "github.com/bpineau/gazetteer/gazetteer"
)

const Name = "myplugin"

type Result struct {
    Score float64 `json:"score"`
}

// IsEmpty implements gazetteer.EmptyReporter.
func (r *Result) IsEmpty() bool { return r == nil || r.Score == 0 }

type Source struct{}

func New() *Source            { return &Source{} }
func (*Source) Name() string  { return Name }
func (*Source) Version() int  { return 1 }

func (*Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
    // ... your logic ...
    return &Result{Score: 0.42}, nil
}

func init() {
    gazetteer.Register(Name, func() any { return &Result{} })
}
```

Wiring it from the caller:

```go
client, _ := gazetteer.NewBuilder().
    With(myplugin.New()).
    Build()
dossier := client.Collect(ctx, listing)

if r, ok := gazetteer.Get[*myplugin.Result](dossier, myplugin.Name); ok {
    fmt.Println(r.Score)
}
```

## Required contract

### `Source` interface

```go
type Source interface {
    Name() string
    Version() int
    Query(ctx context.Context, listing Listing) (any, error)
}
```

- `Name()`: short, lowercase identifier. Must be globally unique
  across every Source registered in a single process.
- `Version()`: monotonic. Bump it whenever Query's logic changes in a
  way that should invalidate downstream caches.
- `Query()`: returns the typed payload — typically a pointer to your
  package's `Result` struct.

### `init()` registration

```go
func init() {
    gazetteer.Register(Name, func() any { return &Result{} })
}
```

This lets `Dossier.UnmarshalJSON` reconstitute concrete typed values
from wire bytes. Without registration, JSON roundtrip silently drops
the payload (`Result.Data` stays nil).

### Error semantics

Wrap one of the framework sentinels so the Client maps Status
correctly:

```go
return nil, fmt.Errorf("myplugin: %w: <detail>", gazetteer.ErrInsufficientInputs)
return nil, fmt.Errorf("myplugin: %w: <detail>", gazetteer.ErrUpstreamUnavailable)
return nil, fmt.Errorf("myplugin: %w: <detail>", gazetteer.ErrAntiBot)
```

See [CONCEPTS.md](CONCEPTS.md) for the full `Status` × sentinel table.

## Optional interfaces

Implement any subset. The framework type-asserts at runtime — there's
no compile-time coupling.

### `EmptyReporter`

```go
type EmptyReporter interface {
    IsEmpty() bool
}
```

When your typed `Data` reports itself as empty AND `Query` returned a
nil error, the framework records `StatusOKEmpty` instead of
`StatusOK`. Lets consumers tell "ran but found nothing" from "had
useful data" without reading the typed payload.

### `Evidencer`

```go
type Evidencer interface {
    Evidence() any
}
```

When `Data.Evidence()` is implemented, the framework stamps
`Result.Evidence` with what it returns. Consumers then read
`dossier.Results["myplugin"].Evidence` without type-asserting on
`Data`. Typical implementation:

```go
type Result struct {
    Score    float64  `json:"score"`
    Evidence Evidence `json:"-"`
}
func (r *Result) Evidence() any { return r.Evidence }
```

### `QueryWither`

```go
type QueryWither interface {
    QueryWith(ctx context.Context, listing Listing, args ...any) (any, error)
}
```

For direct callers that need to pass extras beyond `Listing` — a
pre-resolved upstream id, a session token, a per-call timeout
override. `Client.Collect` always calls plain `Query` — `QueryWith` is
a side-entry path. Implementations should treat unrecognised args as
a fallback to the Listing-only path.

### `BaseURLer`

```go
type BaseURLer interface {
    BaseURL() string
}
```

Used by test harnesses to assert which upstream a Source instance is
pointed at, and by operator-side diagnostics. Return the effective URL
(after `Options.BaseURL` vs. package-level default resolution).

## Reading shared infrastructure from context

The Client propagates HTTP client, logger and debug-dump flag via
`ctx`. Read them with the helpers in `gazetteer/context.go`:

```go
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
    logger := gazetteer.LoggerFrom(ctx).With(slog.String("source", Name))
    httpClient := gazetteer.HTTPClientFrom(ctx)
    if gazetteer.DebugDumpFrom(ctx) {
        // ...
    }
    // ...
}
```

Convention across shipped Sources: prefer an `Options.HTTPClient`
field on your constructor (overrides ctx); fall back to
`HTTPClientFrom(ctx)`; ultimate fallback is `http.DefaultClient`.

## Per-Source caching

Plugins that need cross-run memo (geocode results, zone catalogs,
session tokens) consume a `kvcache.Cache` via their `Options` struct.
See [CACHING.md](CACHING.md).

```go
type Options struct {
    HTTPClient *http.Client
    Cache      kvcache.Cache  // optional; in-memory default
    // ...
}

func NewSource(opts Options) *Source {
    if opts.Cache == nil {
        opts.Cache = memcache.New()
    }
    // ...
}
```

## Circuit breakers

Plugins that scrape an upstream prone to anti-bot, quota or 5xx
storms should wire a `helpers/circuit` breaker into their HTTP path.
See [CIRCUIT_BREAKERS.md](CIRCUIT_BREAKERS.md).

The cross-source canonical sentinel for "I've tripped, stop
scheduling more work":

```go
var ErrCircuitTripped = gazetteer.NewCircuitTrippedError(Name)
```

Returning that pointer makes both per-source matching
(`errors.Is(err, myplugin.ErrCircuitTripped)`) and cross-source
matching (`errors.Is(err, gazetteer.ErrSourceCircuitTripped)`) work.

## Appraisal contributions

If your Source produces a price or rent estimate, or a hazard report,
make your `Result` satisfy the appraisal interfaces so it
automatically contributes to `appraisal.PricePerM2` /
`appraisal.RentValue` / `appraisal.HazardProfile`:

```go
func (r *Result) PriceEstimate() appraisal.PriceEstimate {
    return appraisal.PriceEstimate{
        EurPerM2Cents: r.MedianCents,
        Confidence:    appraisal.ConfidenceMedium,
        SampleSize:    r.N,
        Method:        "myplugin_median",
    }
}
```

Plugin weights default to `appraisal.PriceOptions.DefaultWeight`. Add
an entry under your name in `appraisal.DefaultPriceWeights` (or
override per-call via `PriceOptions.Weights`) if you want a non-default.

## Testing your plugin

The `gazetteer/gazettestest` package provides `StubSource` for
downstream tests that only care about plumbing. For your own tests,
the recommended pattern is `Options.BaseURL` pointing at
`httptest.NewServer`:

```go
srv := httptest.NewServer(...)
defer srv.Close()
src := myplugin.New(myplugin.Options{BaseURL: srv.URL})
```

See [TESTING.md](TESTING.md) for the multi-endpoint + rate-limit
patterns.

## Versioning your wire format

Every typed `Result` should be safe to JSON-marshal and
roundtrip-unmarshal via the registered factory. Tag every wire-visible
field with `json:"..."`. Tag in-process-only fields (Evidence, raw
upstream blobs) with `json:"-"`.

When you change the shape of `Result` in a way that breaks JSON
roundtrip, bump `Version()`. Downstream consumers gating cache
invalidation on `Source.Version()` will automatically discard old
payloads.
