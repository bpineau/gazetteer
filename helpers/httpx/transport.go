package httpx

import (
	"net/http"
)

// composeTransport assembles the layered http.RoundTripper documented in
// doc/specs/chantiers/02-httpx.md §"Composition du RoundTripper":
//
//	client.Do
//	   └─ snapshot       (captures effective response, after cache or net)
//	       └─ cache      (short-circuits hits; revalidates with ETag)
//	           └─ retry  (exp backoff + jitter, honours Retry-After)
//	               └─ rate-limit (per-host token bucket)
//	                   └─ stdlib http.Transport
//
// Layers are skipped (i.e. omitted from the chain) when their config is
// disabled, so a minimal client (no cache, no snapshot, no retry-on-error
// because MaxRetries=0) collapses to ratelimit→stdlib.
func composeTransport(r resolved) http.RoundTripper {
	rt := r.innerTransport

	// Innermost-but-one: rate-limit per host. Always on.
	rt = newRateLimitTransport(rt, r)

	// Retry layer wraps rate-limit so each retry waits on the bucket.
	if r.maxRetries > 0 {
		rt = newRetryTransport(rt, r)
	}

	// Cache layer wraps retry so a cache hit short-circuits everything.
	if r.cacheDir != "" {
		rt = newCacheTransport(rt, r, r.cacheDir)
	}

	// Snapshot layer is outermost so it captures the effective response.
	// We always insert it but it pass-through if no SnapshotDir is set
	// (allowing per-request WithSnapshot to enable it on the fly).
	rt = newSnapshotTransport(rt, r)

	return rt
}
