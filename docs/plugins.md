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
from wire bytes.

- Unregistered Source name on the wire → degraded mode: the envelope
  fields (`Name`, `Status`, `Err`, `Version`, …) are preserved but
  `Result.Data` stays nil. The framework has no factory for the typed
  Result so dropping silently is the only safe move.
- Registered Source name with a payload the factory cannot parse →
  `Dossier.UnmarshalJSON` returns a wrapped error naming the Source.
  This is a schema-drift signal: the persisted bytes were written by
  a different Result shape than the one currently registered.

### Error semantics

Wrap one of the framework sentinels so the Client maps Status
correctly:

```go
return nil, fmt.Errorf("myplugin: %w: <detail>", gazetteer.ErrInsufficientInputs)
return nil, fmt.Errorf("myplugin: %w: <detail>", gazetteer.ErrUpstreamUnavailable)
return nil, fmt.Errorf("myplugin: %w: <detail>", gazetteer.ErrAntiBot)
```

See [concepts.md](concepts.md) for the full `Status` × sentinel table.

A `Query` that panics does not take down the host process: `Collect`
recovers it into a `StatusFailedPermanent` Result carrying the panic
message (permanent because a retry would only reproduce the bug), logs
the stack, and lets the other Sources complete. Treat that as a crash
pad, not a channel: return errors, don't panic.

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

You normally do NOT implement this. The shipped convention is an
`Evidence` **field** on the typed Result:

```go
type Result struct {
    Score    float64  `json:"score"`
    Evidence Evidence `json:"-"`
}
```

and the framework picks that field up reflectively, stamping
`Result.Evidence` on the envelope (and the Dossier JSON `evidence` key)
for you. Implement the `Evidencer` interface only when your provenance
is NOT a plain `Evidence` field — note a type with an `Evidence` field
cannot also have an `Evidence()` method (duplicate name), so the two
mechanisms are mutually exclusive by construction.

## Fetching: use the shared seams

Don't hand-roll the HTTP body of your `Query`:

- **`gazetteer.FetchUpstream(ctx, client, url, spec)`** is the shared GET
  with the error taxonomy baked in — transport errors / 5xx / 429 wrap
  `ErrUpstreamUnavailable` (→ `failed_transient`), other 4xx wrap
  `ErrUpstreamPermanent`, and `FetchSpec.NotFoundBody` maps 404 to your
  "empty payload" so a no-data answer parses instead of failing. Every
  in-tree live source uses it; using it makes your plugin's failures
  classify identically.
- **Expose `Fetcher gazetteer.Fetcher` on your Options** (and consult it
  before FetchUpstream, like the in-tree live sources do): callers inject
  circuit breakers, quota trippers or recorded fixtures through it.
- **`gazetteer.QueryTyped[*Result]`** is the body of your package-level
  `Query` helper and of the `QueryResult` instance method — see any
  in-tree source.go.
- Prefer `Options.BaseURL` fields over mutable package-level URL vars:
  package vars cannot be re-exported by consumer wrappers (writes through
  an alias don't reach your package) and mutate globally.

For **spatial plugins** (point-in-zone datasets), build on
[`helpers/geoindex`](../helpers/geoindex): it owns the compact polygon
wire format, GeoJSON decoding, and the bbox-prefiltered resolve/nearest
index that iris/qpv/encadrement share. For **tiered lookups** (precise →
coarse fallbacks), use [`helpers/fallback`](../helpers/fallback). The full
building-blocks map is [helpers.md](helpers.md).

## Reading shared infrastructure from context

The Client propagates HTTP client and logger via `ctx`. Read them with the helpers in `gazetteer/context.go`:

```go
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
    logger := gazetteer.LoggerFrom(ctx).With(slog.String("source", Name))
    httpClient := gazetteer.HTTPClientFrom(ctx)
    // ...
}
```

Convention across shipped Sources: prefer an `Options.HTTPClient`
field on your constructor (overrides ctx); fall back to
`HTTPClientFrom(ctx)`; ultimate fallback is `http.DefaultClient`.

## Per-Source caching

Plugins that need cross-run memo (geocode results, zone catalogs,
session tokens) consume a `kvcache.Cache` via their `Options` struct.
See [caching.md](caching.md).

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
See [circuit_breakers.md](circuit_breakers.md).

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

See [testing.md](testing.md) for the multi-endpoint + rate-limit
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
