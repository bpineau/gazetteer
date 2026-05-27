// Package httpx is the project's single shared HTTP client.
//
// # Architecture
//
// httpx assembles a layered http.RoundTripper that, from outermost to
// innermost, performs:
//
//	snapshot → cache → retry → rate-limit → stdlib http.Transport
//
// The composite transport delivers per-host token-bucket rate-limiting,
// exponential-backoff retries with Retry-After honour, an on-disk
// persistent HTTP cache with ETag / Last-Modified revalidation, raw
// request/response snapshots for debugging, and atomic file downloads
// with streaming sha256.
//
// # Mental model
//
// Construct a Client with New(Options); pass its Transport() to colly
// (and disable colly's own LimitRule), or use the GetJSON / GetBytes /
// Download helpers directly. The Options struct documents every default
// inline; the Default* constants double as the project's polite-scraping
// specification.
//
// Layers are independent. Leaving HTTPCacheDir empty disables caching;
// leaving SnapshotDir empty disables snapshotting; setting
// RateLimitPerHost very high effectively disables throttling.
//
// # When to reach down a layer
//
// The default GetBytes / GetJSON / Download path covers ~95% of
// callers. The escape hatches, in increasing order of "I really know
// what I'm doing":
//
//   - Per-request: WithSnapshot(ctx, dir) overrides the snapshot
//     directory; WithBypassCache(ctx) skips the cache for one call;
//     WithSource(ctx, name) and WithRunID(ctx, id) tag the snapshot
//     with a source/run for organising output.
//
//   - Per-host: Options.PerHost map keyed on u.Host overrides every
//     default (rate-limit, burst, default TTL, UA, fixed headers).
//
//   - Per-Client: Options.Transport replaces the inner stdlib transport
//     (the most-internal layer). Useful for tests that wire an
//     httptest.Server's transport in, or for HTTP/2 pinning experiments.
//
//   - Last resort: Client.HTTPClient() returns the raw *http.Client.
//     The composite transport still applies — you keep rate-limit,
//     retry, cache and snapshot. Use this for multipart POSTs and any
//     other case GetBytes/Download don't cover.
//
// # Errors
//
// Three typed errors cover the failure modes a caller cares about:
//
//   - *ErrHTTP        — server replied non-2xx (Status, URL, Body)
//   - *ErrTransport   — request never reached a server cleanly
//   - *ErrTooManyRetries — exhausted Options.MaxRetries
//
// All three implement error and play with errors.Is / errors.As. No
// string matching is required (or wanted).
package httpx
