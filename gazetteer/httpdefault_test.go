package gazetteer

import (
	"context"
	"net/http"
	"testing"
)

// TestDefaultHTTPClientIsBounded: the library used to fall back on
// http.DefaultClient, which has NO timeout. An upstream that accepts the TCP
// connection and never answers then pinned the Source, and the Collect waiting
// on it, for the life of the process. Collect's own per-Source timeout being
// opt-in and unset by default, nothing else bounded the atomic Query and
// hand-built Builder paths that AGENTS.md puts forward.
func TestDefaultHTTPClientIsBounded(t *testing.T) {
	if DefaultHTTPClient.Timeout != DefaultHTTPTimeout {
		t.Errorf("DefaultHTTPClient.Timeout = %v, want %v", DefaultHTTPClient.Timeout, DefaultHTTPTimeout)
	}
	if DefaultHTTPTimeout <= 0 {
		t.Fatal("DefaultHTTPTimeout must be strictly positive")
	}
	if got := HTTPClientFrom(context.Background()); got.Timeout <= 0 {
		t.Errorf("HTTPClientFrom(empty ctx) has no timeout (%#v)", got)
	}
	if got := NewBuilder().httpClient; got.Timeout <= 0 {
		t.Errorf("NewBuilder()'s default client has no timeout (%#v)", got)
	}

	// An explicit client still wins, timeout or not: the default is a floor
	// for callers who configure nothing, never an override.
	custom := &http.Client{}
	if got := HTTPClientFrom(WithHTTPClient(context.Background(), custom)); got != custom {
		t.Error("an explicit ctx client must win over the default")
	}
	if got := NewBuilder().WithHTTPClient(custom).httpClient; got != custom {
		t.Error("Builder.WithHTTPClient must win over the default")
	}
}
