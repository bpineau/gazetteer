# Caching

Gazetteer separates *transport-level* caching (handled by
`helpers/httpx`) from *semantic* caching (handled by
`helpers/kvcache`). This document is about the second: persistent
key/value memoisation for derived values that survive across process
runs.

## The `kvcache.Cache` contract

```go
type Cache interface {
    Get(ctx context.Context, key string) (Entry, error)
    Set(ctx context.Context, e Entry) error
    DeleteExpired(ctx context.Context, now time.Time) (int64, error)
}
```

```go
type Entry struct {
    Key       string
    Value     []byte
    FetchedAt time.Time
    ExpiresAt *time.Time
}
```

Contract details:

- **`Get` returns ErrNotFound** for missing keys; callers MUST use
  `errors.Is(err, kvcache.ErrNotFound)`.
- **`Get` returns expired rows.** The store is a
  stale-while-revalidate buffer; the caller decides what to do with
  `ExpiresAt`.
- **`Set` fills `FetchedAt`** when zero — callers don't need to
  bookkeep.
- **`ExpiresAt == nil` means "no TTL"** — kept until overwritten.
- **`DeleteExpired`** is the only operation that enforces TTL.
  Returns the number of rows removed; rows with nil ExpiresAt are
  untouched.
- **Implementations are safe for concurrent use** from multiple
  goroutines.

## Conformance suite

Every `Cache` implementation should run the conformance suite from
`helpers/kvcache/kvcachetest`:

```go
package memcache_test

import (
    "testing"

    "github.com/bpineau/gazetteer/helpers/kvcache"
    "github.com/bpineau/gazetteer/helpers/kvcache/kvcachetest"
    "github.com/bpineau/gazetteer/helpers/kvcache/memcache"
)

func TestSuite(t *testing.T) {
    kvcachetest.Suite(t, func(t *testing.T) kvcache.Cache {
        return memcache.New()
    })
}
```

The suite covers:

- `Get` on a missing key returns `ErrNotFound`
- Round-trip of Key, Value, FetchedAt, ExpiresAt
- Upsert: same-key `Set` replaces the prior row
- Expired rows are still returned by `Get` (stale-while-revalidate)
- `Set` fills `FetchedAt` when zero
- `DeleteExpired` counts accurately, leaves fresh + permanent rows
- Concurrent `Get`/`Set`/`DeleteExpired` does not race
- The `kvcache.Set` + `WithTTL` helper populates `ExpiresAt`

Run with `-race` to exercise the parallel-access sub-test.

## Backends

### `helpers/kvcache/memcache` — in-memory

```go
import "github.com/bpineau/gazetteer/helpers/kvcache/memcache"

c := memcache.New() // fresh, empty, concurrent-safe
```

Persistence: none. Use for tests, one-shot CLI tools, and short-lived
processes that do not need cross-run memo.

### `gazetteer.NewKVMemCache()` — convenience re-export

```go
cache := gazetteer.NewKVMemCache()
```

Same backend as `memcache.New()`; saves callers a second import line.

### Persistent backends

Out-of-scope for the library — implement the interface against any
SQL / KV store (Bun, sqlite, Redis, …). The conformance suite gives
you a hardness contract; pass it and the library considers your
backend wired.

## The `Set` helper

`kvcache.Set` packages key + value + TTL into one call:

```go
err := kvcache.Set(ctx, c, "geocode:ban:abc", payload,
    kvcache.WithTTL(365 * 24 * time.Hour))
```

Equivalent to:

```go
exp := time.Now().Add(365 * 24 * time.Hour)
err := c.Set(ctx, kvcache.Entry{Key: "geocode:ban:abc", Value: payload, ExpiresAt: &exp})
```

Callers that need fine-grained control (set FetchedAt explicitly,
write multiple rows atomically, …) use `Cache.Set` directly.

## Where Sources need a `Cache`

Some Sources expose a `Cache` slot on their Options:

| Source              | What it caches                             | Default fallback |
|---------------------|--------------------------------------------|------------------|
| `helpers/banx.CachedGeocoder` | BAN forward + reverse responses    | none — caller supplies |
| `sources/dvf.SectionDiscoverer` | per-INSEE cadastral section lists | in-memory memcache (`Options.SectionCache == nil`) |

Plugins that scrape per-zone data typically follow the same pattern
(see [plugins.md](plugins.md)). A persistent backend is recommended
in production; the in-memory default is fine for tests.

## Cache-key namespacing

There is no namespace enforcement at the library level — every Source
prefixes its keys with its name:

```
geocode:ban:<sha256(query)>
geocode:ban_reverse:<sha256(lat,lon)>
dvf:sections:<insee>
myplugin:<id>
```

The sha256 truncation to 16 bytes (`hex.EncodeToString(h[:16])`) is
the project convention — long enough that collisions are
statistically impossible at our cardinalities, short enough to keep
keys readable.

## Soft vs hard failures

A Source memoising upstream responses should distinguish:

- **Soft failures** (404, "no data") — cache them. The upstream
  answer is "permanently nothing" for these inputs.
- **Hard failures** (transport, 5xx, 429) — DO NOT cache. The upstream
  was unavailable; retrying later may succeed.

Conflating the two locks a queue against a transiently-broken upstream
for the full TTL.
