# Testing patterns

How to write reliable tests against gazetteer code. The patterns
below come from the in-tree Source test suites; reuse them for
out-of-tree plugins and downstream applications.

## Stubbing out a `Source` with `gazettestest.StubSource`

When you need a deterministic Source for a downstream test:

```go
import (
    "context"
    "testing"

    "github.com/bpineau/gazetteer/gazetteer"
    "github.com/bpineau/gazetteer/gazetteer/gazettestest"
)

func TestMyAppHandlesDossier(t *testing.T) {
    src := gazettestest.NewStubSource("myname", 1, &MyResult{Score: 42}, nil)
    client, _ := gazetteer.NewBuilder().With(src).Build()

    d := client.Collect(context.Background(), gazetteer.Listing{})
    r, _ := gazetteer.Get[*MyResult](d, "myname")
    if r.Score != 42 { t.Fatalf(...) }
}
```

Pass a non-nil err to exercise the framework's `Status` mapping:

```go
src := gazettestest.NewStubSource("myname", 1, nil, gazetteer.ErrAntiBot)
// client.Collect → d.Results["myname"].Status == gazetteer.StatusFailedAntiBot
```

Tests that need to assert on Query arguments (e.g. "was Query called
with this Listing?") should hand-roll their own mock — `StubSource`
ignores its inputs by design.

## Pointing a Source at `httptest.NewServer`

Sources that hit HTTP expose an `Options.BaseURL` field. Wire it to
an `httptest.Server`:

```go
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    fmt.Fprint(w, fixtureJSON)
}))
defer srv.Close()

src := ademe.NewSource(ademe.Options{
    BaseURL: srv.URL,
})
result, err := src.Query(ctx, listing)
```

Sources with multiple endpoints (DVF: section catalog + mutations
API) take a single `httptest.Server` and route by path:

```go
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    switch {
    case strings.HasPrefix(r.URL.Path, "/cadastre/"):
        // serve section catalog
    case strings.HasPrefix(r.URL.Path, "/dvf/mutations/"):
        // serve mutations
    }
}))
```

## Bypassing httpx rate-limit + retries in cascade tests

A Source that fans out to many sub-URLs (e.g. one per cadastral
section) needs the test httpx Client to **not** rate-limit per host:
the default of 1 req/s makes the test take forever, and the retry
layer's 30 s backoff makes a single 5xx fixture sit and wait.

The recipe:

```go
import "github.com/bpineau/gazetteer/helpers/httpx"

hc, err := httpx.New(httpx.Options{
    RateLimitPerHost: 1000,   // effectively disabled
    MaxRetries:       -1,     // disabled — test sees the raw error
    // no HTTPCacheDir → caching disabled
})
```

Without these, a multi-endpoint fan-out test will frequently hang on
the test timeout.

## Faking a `Normalizer`

The Normalizer is an interface — test doubles are trivial:

```go
type fakeNormalizer struct{}

func (fakeNormalizer) Normalize(_ context.Context, addr string) (gazetteer.Listing, error) {
    return gazetteer.Listing{
        Address: addr,
        Zip:     "75001",
        INSEE:   "75101",
    }, nil
}

client, _ := gazetteer.NewBuilder().
    WithNormalizer(fakeNormalizer{}).
    With(stubSource).
    Build()

l, _ := client.Normalize(ctx, "1 rue X")
```

`gazetteer.BANNormalizer` is also testable: pass any `banx.Geocoder`
stub and `nil` for the communes table.

## Faking a `banx.Geocoder`

The interface is one method:

```go
type fakeGeocoder struct {
    insee, label string
    lat, lon     float64
}

func (f fakeGeocoder) Geocode(_ context.Context, q banx.GeocodeQuery) (banx.GeocodeResult, error) {
    return banx.GeocodeResult{
        Label: f.label, CityCode: f.insee, PostCode: "75001",
        Lat: f.lat, Lon: f.lon,
    }, nil
}

src, _ := dvf.NewSource(dvf.Options{
    HTTP:     httpClient,
    Geocoder: fakeGeocoder{insee: "75101", label: "1 rue X 75001 Paris"},
})
```

To also test the reverse cascade, embed both methods in the same
type.

## Conformance suite for a custom cache

When implementing `kvcache.Cache` for a new backend, run the
conformance suite:

```go
func TestMyBackend(t *testing.T) {
    kvcachetest.Suite(t, func(t *testing.T) kvcache.Cache {
        return NewMyBackend(t.TempDir())
    })
}
```

See [caching.md](caching.md).

## Testing Source registration

Registration via `init()` is global. A test that registers under a
name another in-tree Source already uses will panic — use a unique
name in tests:

```go
gazetteer.Register("ex-myplugin", func() any { return &MyResult{} })
```

To inspect the registry from a test:

```go
names := gazetteer.RegisteredNames()
factory := gazetteer.Lookup("ademe")
```

## Resetting circuit-breaker state between tests

```go
circuit.ResetCircuitTripCountersForTest()
circuit.ResetCircuitStateRegistryForTest()
```

Use a `t.Cleanup` to reset after each test so a stuck breaker
doesn't leak across subtests.

## Sanity: run with `-race` and a timeout

```bash
go test -timeout 120s -race ./...
```

The `Collect` parallel fan-out, the circuit-breaker counters, the
in-memory cache and the OSM catalog hot-swap all rely on tested
concurrency invariants. Run with `-race` once per CI cycle to catch
regressions.
