package httpx

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestRetry_NonIdempotentIsSentOnce pins that a POST answered 500 is not
// replayed (the server may have applied it), unless the caller opts in or
// the request carries an Idempotency-Key; a GET keeps its retries.
func TestRetry_NonIdempotentIsSentOnce(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(t, Options{RateLimitPerHost: 1000, MaxRetries: 1, BaseRetryInterval: 1, BackoffCap: 1})

	send := func(ctx context.Context, method string, hdr http.Header) int32 {
		t.Helper()
		atomic.StoreInt32(&hits, 0)
		req, err := http.NewRequestWithContext(ctx, method, srv.URL, strings.NewReader("x"))
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range hdr {
			req.Header[k] = v
		}
		if resp, err := c.HTTPClient().Do(req); err == nil {
			_ = resp.Body.Close()
		}
		return atomic.LoadInt32(&hits)
	}
	ctx := context.Background()
	if n := send(ctx, http.MethodPost, nil); n != 1 {
		t.Errorf("POST: %d upstream executions, want 1", n)
	}
	if n := send(WithRetryUnsafe(ctx), http.MethodPost, nil); n != 2 {
		t.Errorf("POST with WithRetryUnsafe: %d executions, want 2", n)
	}
	if n := send(ctx, http.MethodPost, http.Header{"Idempotency-Key": {"k1"}}); n != 2 {
		t.Errorf("POST with Idempotency-Key: %d executions, want 2", n)
	}
	if n := send(ctx, http.MethodGet, nil); n != 2 {
		t.Errorf("GET: %d executions, want 2 (retries kept for reads)", n)
	}
}
