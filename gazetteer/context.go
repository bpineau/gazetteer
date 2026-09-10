package gazetteer

import (
	"context"
	"log/slog"
	"net/http"
	"time"
)

type ctxKey int

const (
	ctxKeyHTTPClient ctxKey = iota
	ctxKeyLogger
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
//  3. If neither is set, HTTPClientFrom returns DefaultHTTPClient.
//
// Source authors implementing a new Source SHOULD follow pattern (1)
// + (2). The default (3) is a safety net, not a recommendation.
func WithHTTPClient(ctx context.Context, c *http.Client) context.Context {
	return context.WithValue(ctx, ctxKeyHTTPClient, c)
}

// DefaultHTTPTimeout bounds one upstream request made through
// DefaultHTTPClient, end to end (connection, headers, body).
const DefaultHTTPTimeout = 60 * time.Second

// DefaultHTTPClient is the client the library falls back on when a caller
// configures none. It is http.DefaultClient's transport plus a
// DefaultHTTPTimeout deadline.
//
// The deadline is the whole point: http.DefaultClient has NO timeout, so an
// upstream that accepts the TCP connection and then never answers pins the
// Source, and the Collect waiting on it, for the life of the process. Collect's
// own per-Source timeout is opt-in (WithPerSourceTimeout) and unset by default,
// so nothing else bounds the atomic Query and hand-built Builder paths.
//
// Override it per Source with Options.HTTPClient, per Client with
// Builder.WithHTTPClient (factory.NewDefault already passes a polite, cached,
// rate-limited one), or process-wide by assigning to this variable before
// building anything.
var DefaultHTTPClient = &http.Client{Timeout: DefaultHTTPTimeout}

// HTTPClientFrom returns the HTTP client set on ctx, or DefaultHTTPClient
// if none is set. See WithHTTPClient for the per-Source precedence
// convention.
func HTTPClientFrom(ctx context.Context) *http.Client {
	if c, ok := ctx.Value(ctxKeyHTTPClient).(*http.Client); ok && c != nil {
		return c
	}
	return DefaultHTTPClient
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
