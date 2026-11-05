package httpx

import (
	"context"
	"sync/atomic"
)

// Non-exported context keys to avoid collisions with caller code.
type ctxKey int

const (
	ctxKeySource ctxKey = iota + 1
	ctxKeyRunID
	ctxKeyBypassCache
	ctxKeySnapshotDir
	ctxKeyRetryUnsafe
)

// WithSource tags the context with a source name (e.g. "licitor"), used by
// the snapshot middleware to organise files under <SnapshotDir>/<source>/...
func WithSource(ctx context.Context, src string) context.Context {
	if src == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKeySource, src)
}

// SourceFromContext returns the value set by WithSource, or "" if unset.
func SourceFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeySource).(string); ok {
		return v
	}
	return ""
}

// WithRunID tags the context with a run ID (typically a ULID/UUID), used to
// group all requests of a single scrape run under the same snapshot folder.
func WithRunID(ctx context.Context, id string) context.Context {
	if id == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyRunID, id)
}

// RunIDFromContext returns the value set by WithRunID, or "" if unset.
func RunIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRunID).(string); ok {
		return v
	}
	return ""
}

// WithBypassCache tells the cache middleware to skip the cache for this
// request — neither read nor write. Useful for debug runs that must hit
// the network without polluting the cache.
func WithBypassCache(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeyBypassCache, true)
}

// BypassCacheFromContext reports whether WithBypassCache was applied.
func BypassCacheFromContext(ctx context.Context) bool {
	v, ok := ctx.Value(ctxKeyBypassCache).(bool)
	return ok && v
}

// WithSnapshot overrides the snapshot directory for this request, even
// when the global Options.SnapshotDir is empty.
func WithSnapshot(ctx context.Context, dir string) context.Context {
	if dir == "" {
		return ctx
	}
	return context.WithValue(ctx, ctxKeySnapshotDir, dir)
}

// WithRetryUnsafe lets the retry layer replay this request even though its
// method is not idempotent (POST, PATCH). Only a caller that knows the
// request has no side effect upstream (an Overpass query sent as a POST
// form, a search endpoint) should set it: by default a non-idempotent
// request is sent once, since a 5xx or a lost response does not prove the
// server did not apply it. A request carrying an Idempotency-Key header is
// replayed without this option.
func WithRetryUnsafe(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeyRetryUnsafe, true)
}

// RetryUnsafeFromContext reports whether WithRetryUnsafe was applied.
func RetryUnsafeFromContext(ctx context.Context) bool {
	v, ok := ctx.Value(ctxKeyRetryUnsafe).(bool)
	return ok && v
}

// attemptsKey carries the *atomic.Int32 the retry layer bumps on every
// upstream send of one request, so GetBytes and the snapshot layer, which
// sit above it, can report how many sends it took.
type attemptsKey struct{}

// withAttemptCounter returns ctx carrying a fresh attempt counter, or ctx
// itself when one is already present (the snapshot layer and GetBytes then
// share it).
func withAttemptCounter(ctx context.Context) (context.Context, *atomic.Int32) {
	if n, ok := ctx.Value(attemptsKey{}).(*atomic.Int32); ok {
		return ctx, n
	}
	n := new(atomic.Int32)
	return context.WithValue(ctx, attemptsKey{}, n), n
}

// countAttempt records one upstream send on ctx's counter, if any.
func countAttempt(ctx context.Context) {
	if n, ok := ctx.Value(attemptsKey{}).(*atomic.Int32); ok {
		n.Add(1)
	}
}

// attemptsOf reads a counter for a report: the sends the retry layer made,
// or 1 when it made none (retries disabled, a cache hit, a trip shared with
// another caller), since the response was then obtained in one go.
func attemptsOf(n *atomic.Int32) int {
	return max(int(n.Load()), 1)
}
