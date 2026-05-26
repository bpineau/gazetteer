testdata for httpx tests
========================

This directory holds golden fixtures consumed by `httpx_test.go`. Each
sub-directory is a self-contained scenario:

- `cache_fresh/` : a cache entry whose `expires_at` is in the future,
  so the cache layer serves it without contacting the inner transport.
  Files:
    * `<hash>.json` — meta (URL, status, headers, expiry).
    * `<hash>.body` — body bytes.

- `cache_stale/` : a cache entry whose `expires_at` is in the past but
  whose meta carries an `Etag`. The cache layer uses this to issue an
  `If-None-Match` conditional GET — the inner transport responds 304 and
  the body is served from disk again.

The tests synthesise these fixtures at runtime (using `cacheTransport.writeEntry`)
to keep the fixtures self-describing and resistant to schema drift; this
directory is therefore intentionally light. To inspect a written entry inside
the per-test temp dir Go gives you:

    cat $TMPDIR/<test-name>/$(printf %s | sha256sum ...)/*.json | jq
