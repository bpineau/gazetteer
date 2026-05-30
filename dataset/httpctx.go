package dataset

import (
	"context"

	"github.com/bpineau/gazetteer/helpers/httpx"
)

// ctxKey is the unexported context key type for the injected HTTP client.
type ctxKey int

const clientKey ctxKey = 0

// WithHTTPClient returns a context carrying c. Refresh uses it to make the
// batch's HTTP client available to Transforms that fetch their own inputs
// from a live API (rather than declaring static Raw URLs the engine
// downloads) — e.g. a Source whose dataset is built from per-area API
// queries. Transforms over plain downloaded files never need it.
func WithHTTPClient(ctx context.Context, c *httpx.Client) context.Context {
	return context.WithValue(ctx, clientKey, c)
}

// HTTPClient returns the *httpx.Client injected by Refresh, or nil when none
// is set. A Transform that requires live fetching should return a clear
// error when this is nil.
func HTTPClient(ctx context.Context) *httpx.Client {
	c, _ := ctx.Value(clientKey).(*httpx.Client)
	return c
}
