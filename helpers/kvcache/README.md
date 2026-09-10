# kvcache — pluggable persistent key/value cache contract

A backend-agnostic persistent key/value cache interface used across the
project: the BAN geocoder cache, the DVF per-commune section cache, and
any caller that needs a memo with optional TTL.

The contract is deliberately the smallest surface that still supports
two production needs:

- **Stale-while-revalidate**: `Get` returns expired rows (with their
  `ExpiresAt`), so the caller decides whether an expired answer is
  still good enough while a refresh runs.
- **Out-of-band garbage collection**: `DeleteExpired` lets an operator
  cron reclaim space without the read path paying for it.

The in-memory backend is additionally **bounded**: `memcache` keeps
`DefaultMaxEntries` rows (override with `memcache.WithMaxEntries(n)`,
`n <= 0` for unlimited) and, at the ceiling, sweeps the expired rows before
evicting the least-recently-used one. Below the ceiling nothing is swept, so
stale-while-revalidate reads keep working.

## Quick start

```go
import (
    "github.com/bpineau/gazetteer/helpers/kvcache"
    "github.com/bpineau/gazetteer/helpers/kvcache/memcache"
)

c := memcache.New() // in-memory reference backend

_ = kvcache.Set(ctx, c, "geocode:ban:abc", payload,
    kvcache.WithTTL(365*24*time.Hour))

row, err := c.Get(ctx, "geocode:ban:abc")
switch {
case errors.Is(err, kvcache.ErrNotFound):
    // miss
case err == nil && row.ExpiresAt != nil && row.ExpiresAt.Before(time.Now()):
    // stale hit: usable, but schedule a refresh
}
```

## Bringing your own backend

Implement the `Cache` interface against any SQL / KV store, then run
the conformance suite in `helpers/kvcache/kvcachetest` to validate the
semantics (miss vs expired, TTL rounding, delete counts):

```go
func TestMyBackend(t *testing.T) {
    kvcachetest.Suite(t, func(t *testing.T) kvcache.Cache { return myNew(t) })
}
```

Persistent backends are intentionally out-of-scope for the library; the
in-memory `memcache` reference backend is fine for tests, for short-lived
processes and, being bounded, for the process-lifetime memos a long-lived
server keeps.

## Public API

See `go doc github.com/bpineau/gazetteer/helpers/kvcache`:

- `type Cache interface`, `type Entry`
- `memcache.New(...Option)`, `memcache.WithMaxEntries(int)`,
  `memcache.DefaultMaxEntries`
- `func Set(ctx, Cache, key, value, ...SetOption) error`,
  `func WithTTL(time.Duration) SetOption`
- `var ErrNotFound`

## Status

Stable. Symbols may be added but not renamed or removed without a
deprecation cycle.
