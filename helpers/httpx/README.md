# httpx — opinionated, layered HTTP client

The single shared HTTP client for every outbound call in the project. A
composite `http.RoundTripper` stacks per-host token-bucket rate-limiting,
exponential-backoff retries that honour `Retry-After`, an on-disk
persistent cache with ETag / Last-Modified revalidation, and a raw
request/response snapshot tap — all behind a small, opinionated `Options`
struct.

```
            +-------- caller --------+
            |  GetBytes / GetJSON /  |
            |  Download / Transport  |
            +-----------+------------+
                        |
                        v
   snapshot → cache → retry → rate-limit → http.Transport
                        |
                        v
                       network
```

## Why this package exists

Without a shared HTTP client, every scraper ends up wiring its own
`*http.Client` with its own retry loop, its own UA, its own polite
per-host rate-limit estimate, its own debug snapshot scheme — and the
copies drift: a fix in one parser regresses in another. `httpx` is the
one place those layers live; every consumer embeds it.

## Principles

- **Opinionated defaults.** Chrome 147 UA, `Sec-Ch-Ua` / `Sec-Fetch-*`
  client hints copied from a real browser, 2 req/s per host with burst
  4, 5 retries with exponential backoff capped at 60 s, 6 h cache
  fallback TTL, 50 MiB body cap. The defaults survived 6+ months of
  production scraping against DataDome / Cloudflare backends.
- **Every escape hatch is reachable.** `Options.PerHost` overrides any
  default per-host. `Options.Transport` replaces the inner stdlib
  transport (useful for tests with `httptest.Server`). `HTTPClient()`
  returns the raw `*http.Client` for the rare multipart POST.
  `WithBypassCache` / `WithSnapshot` opt-out per-request.
- **Layers are independent.** Drop the cache by leaving `HTTPCacheDir`
  empty; drop snapshots by leaving `SnapshotDir` empty; drop
  rate-limiting by setting `RateLimitPerHost` extremely high. No
  flag fiddling required.
- **Errors are typed.** `*ErrHTTP`, `*ErrTransport`,
  `*ErrTooManyRetries` all play with `errors.Is` / `errors.As`. No
  string matching, ever.
- **Context is canonical.** Every public function takes
  `context.Context` first. Snapshot scope, run-id scope and
  cache-bypass are all set via `context.WithValue` helpers
  (`WithSnapshot`, `WithRunID`, `WithBypassCache`) — composable across
  middleware boundaries.

## Quick start

```go
import (
    "context"
    "github.com/bpineau/gazetteer/helpers/httpx"
)

cli, _ := httpx.New(httpx.Options{
    HTTPCacheDir: "data/cache/http",
    SnapshotDir:  "data/snapshots",
    PerHost: map[string]httpx.HostOptions{
        "api.example.fr": {RateLimit: ptr(0.5)}, // 1 req / 2 s for that host
    },
})
defer cli.Close()

var payload struct{ Items []string }
if err := cli.GetJSON(ctx, "https://api.example.fr/v1/list", nil, &payload); err != nil {
    // *ErrHTTP for 4xx/5xx, *ErrTransport for DNS/dial/TLS, *ErrTooManyRetries when MaxRetries is hit.
}
```

For a colly Collector:

```go
collector := colly.NewCollector()
collector.WithTransport(cli.Transport())
// IMPORTANT: also disable colly's own LimitRule — httpx is already
// rate-limiting per host.
```

## Public API

See `go doc github.com/bpineau/gazetteer/helpers/httpx` for the
godoc-rendered surface. The headline types and functions:

- `func New(Options) (*Client, error)`
- `(*Client) GetBytes(ctx, url, http.Header) ([]byte, *Response, error)`
- `(*Client) GetJSON(ctx, url, http.Header, any) error`
- `(*Client) Download(ctx, url, dest, DownloadOptions) (DownloadResult, error)`
- `(*Client) Transport() http.RoundTripper` — for colly / custom callers
- `(*Client) HTTPClient() *http.Client` — escape hatch for multipart, etc.
- `type Options struct { … }` and `type HostOptions struct { … }`
- `*ErrHTTP`, `*ErrTransport`, `*ErrTooManyRetries` (use `errors.As`)
- Context helpers: `WithSource`, `WithRunID`, `WithSnapshot`, `WithBypassCache`
  (and the matching `*FromContext` getters)

The `Default*` constants document the defaults verbatim — read them as
the spec rather than reading the resolver.

## When to reach down a layer

- **Replace the inner transport.** Set `Options.Transport` (commonly an
  `httptest.Server`'s transport for tests, or a custom `http2.Transport`
  for HTTP/2 pinning experiments).
- **Skip the cache for one request.** Wrap the context with
  `httpx.WithBypassCache(ctx)` — the cache layer reads neither nor writes.
- **Snapshot a single run.** Wrap the context with
  `httpx.WithSnapshot(ctx, dir)` (or set `Options.SnapshotDir` globally).
  The snapshot middleware writes raw request + response under
  `<dir>/<source>/<runID>/...` using the source/run from the same
  context.
- **Multipart POST or anything else `GetBytes`/`Download` doesn't
  cover.** Call `cli.HTTPClient().Do(req)` — you keep the composite
  transport (rate-limit, retry, cache, snapshot all still apply).

## Status

Stable. ~600 LOC production code + ~1100 LOC tests. The `Default*`
constants document the on-the-wire behaviour — read them as the spec
rather than reading the resolver. Symbols may be added but not renamed
or removed without a deprecation cycle.
