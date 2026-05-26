package gazetteer

import (
	"context"
	"log/slog"
	"net/http"
)

type ctxKey int

const (
	ctxKeyHTTPClient ctxKey = iota
	ctxKeyLogger
	ctxKeyDebugDump
	ctxKeyCache
)

// WithHTTPClient stores an HTTP client in ctx for Sources to read with
// HTTPClientFrom. Callers normally don't set this — the Builder propagates
// its configured client into ctx before invoking Source.Query.
//
// # Sources: where does my HTTP client come from?
//
// The convention across shipped Sources is:
//
//  1. If `Options.HTTPClient` is non-nil, use it (typed
//     `*http.Client` or `*httpx.Client`). Most Sources expose this
//     field so individual tests can swap a fake transport without
//     building a full Builder.
//  2. Otherwise, fall back to HTTPClientFrom(ctx) — the Builder
//     propagates the configured client this way so the same Client
//     instance is shared across every Source in a Collect call.
//  3. If neither is set, HTTPClientFrom returns http.DefaultClient.
//
// Source authors implementing a new Source SHOULD follow pattern (1)
// + (2). The default (3) is a safety net, not a recommendation.
func WithHTTPClient(ctx context.Context, c *http.Client) context.Context {
	return context.WithValue(ctx, ctxKeyHTTPClient, c)
}

// HTTPClientFrom returns the HTTP client set on ctx, or http.DefaultClient
// if none is set. See WithHTTPClient for the per-Source precedence
// convention.
func HTTPClientFrom(ctx context.Context) *http.Client {
	if c, ok := ctx.Value(ctxKeyHTTPClient).(*http.Client); ok && c != nil {
		return c
	}
	return http.DefaultClient
}

// WithLogger stores a *slog.Logger in ctx.
func WithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, ctxKeyLogger, l)
}

// LoggerFrom returns the logger set on ctx, or slog.Default() if none.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(ctxKeyLogger).(*slog.Logger); ok && l != nil {
		return l
	}
	return slog.Default()
}

// WithDebugDump enables raw request/response logging for sources that
// honour the flag.
func WithDebugDump(ctx context.Context, on bool) context.Context {
	return context.WithValue(ctx, ctxKeyDebugDump, on)
}

// DebugDumpFrom reports whether debug dump is enabled. Default: false.
func DebugDumpFrom(ctx context.Context) bool {
	v, _ := ctx.Value(ctxKeyDebugDump).(bool)
	return v
}

// WithCache stores a Cache on ctx.
func WithCache(ctx context.Context, c Cache) context.Context {
	return context.WithValue(ctx, ctxKeyCache, c)
}

// CacheFrom returns the Cache set on ctx, or a process-wide MemCache
// fallback if none. The fallback is shared across calls so cache hits
// persist for the lifetime of the process.
func CacheFrom(ctx context.Context) Cache {
	if c, ok := ctx.Value(ctxKeyCache).(Cache); ok && c != nil {
		return c
	}
	return defaultCache
}

var defaultCache Cache = NewMemCache(4096)
