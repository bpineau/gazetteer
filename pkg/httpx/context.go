package httpx

import "context"

// Non-exported context keys to avoid collisions with caller code.
type ctxKey int

const (
	ctxKeySource ctxKey = iota + 1
	ctxKeyRunID
	ctxKeyBypassCache
	ctxKeySnapshotDir
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
