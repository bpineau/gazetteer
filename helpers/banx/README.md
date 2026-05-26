# banx — BAN (Base Adresse Nationale) client

A small, opinionated Go client for the French national address service
at `api-adresse.data.gouv.fr`. It bundles:

- **Forward geocoding** — free-form FR address → lat/lon + INSEE
  commune code.
- **Reverse geocoding** — lat/lon → INSEE + canonical label.
- **A persistent cache layer** over any `kvcache.Cache` backend
  (in-memory for tests, on-disk for production).
- **An `INSEEResolver` cascade** that tries forward first and falls
  back to reverse when the caller knows the coordinates.

Designed to be the smallest reusable piece for any French
real-estate / mobility / public-data app.

## Architecture

```
            caller
              |
              v
   +-- INSEEResolver -----------------+
   |   forward → reverse cascade      |
   +-+--------------------------------+
     |
     v
   +-- CachedGeocoder ----------------+
   |   kvcache lookup → delegate      |
   |   coherence-guarded writeback    |
   +-+--------------------------------+
     |
     v
   +-- BANClient --------------------+
   |   Geocode(GeocodeQuery)         |
   |   Reverse(lat, lon)             |
   +-+-------------------------------+
     |
     v
   api-adresse.data.gouv.fr
```

## Example

```go
package main

import (
    "context"
    "fmt"

    "myrepo/pkg/banx"
    "myrepo/pkg/httpx"
    "myrepo/pkg/kvcache/memcache"
)

func main() {
    httpClient := httpx.New(httpx.Options{})
    raw := banx.NewBANClient(httpClient)
    cached := banx.NewCachedGeocoder(raw, memcache.New(), 0 /* default 1y */)

    resolver := &banx.INSEEResolver{
        Forward:         cached,
        Reverse:         raw,           // BANClient also implements ReverseGeocoder
        MinForwardScore: 0.7,
    }

    res, err := resolver.Resolve(context.Background(), banx.INSEEQuery{
        Address: "3 Impasse de Mont Louis",
        City:    "Paris",
        Zip:     "75011",
    })
    if err != nil {
        // handle banx.ErrNotFound or transport errors
        return
    }
    fmt.Printf("INSEE=%s lat=%.5f lon=%.5f via=%s\n",
        res.INSEE, res.Lat, res.Lon, res.Source)
}
```

## API

| Symbol | Purpose |
|--------|---------|
| `Geocoder` / `ReverseGeocoder` interfaces | Substitution points (HTTP client, fake, etc.). |
| `GeocodeQuery`, `GeocodeResult` | Forward request / response shapes. |
| `BANClient` + `NewBANClient(httpx.Client)` | Direct HTTP client. |
| `CachedGeocoder` + `NewCachedGeocoder` | Cache decorator (any `kvcache.Cache`). |
| `INSEEResolver` + `INSEEQuery` / `INSEEResolution` | The forward→reverse cascade. |
| `ErrNotFound` | Sentinel for empty BAN results. |
| `ErrIncoherentBANResponse` | Sentinel raised by the coherence guard. |
| `Unwrapper` | Decorator unwrap contract. |
| `CacheKey` / `ReverseCacheKey` | Stable cache-key helpers (prefix `geocode:ban:` and `geocode:ban_reverse:`). |

## Dependencies

- `myrepo/pkg/httpx` — the shared HTTP client.
- `myrepo/pkg/kvcache` — the cache backend interface (only used by
  `CachedGeocoder`; the raw `BANClient` itself has no kvcache dep).

No transitive dependency on any other internal package.

## Notes

- The cache key namespaces (`geocode:ban:`, `geocode:ban_reverse:`)
  are kept as-is even though the package was renamed from `geocode` to
  `banx`, so existing persistent caches stay warm.
- BAN forward scores below 0.7 are not trusted by default — adjust
  `INSEEResolver.MinForwardScore` to tune.
- The `banMaxQueryLen` constant caps the `q` parameter at BAN's
  200-char limit; over-long inputs are truncated rather than rejected
  so a single bad address doesn't cascade.
