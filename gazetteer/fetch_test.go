package gazetteer

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/helpers/httpx"
)

func fetchTestServer(t *testing.T, status int, body string, gotAccept *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gotAccept != nil {
			*gotAccept = r.Header.Get("Accept")
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchUpstreamOK(t *testing.T) {
	var accept string
	srv := fetchTestServer(t, http.StatusOK, `{"ok":true}`, &accept)
	body, err := FetchUpstream(context.Background(), srv.Client(), srv.URL,
		FetchSpec{Prefix: "demo", Accept: "application/json"})
	if err != nil {
		t.Fatalf("FetchUpstream: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Errorf("body = %q", body)
	}
	if accept != "application/json" {
		t.Errorf("Accept header = %q, want application/json", accept)
	}
}

func TestFetchUpstreamStatusTaxonomy(t *testing.T) {
	cases := []struct {
		name         string
		status       int
		notFoundBody []byte
		wantErr      error
		wantBody     string
	}{
		{"5xx_transient", 502, nil, ErrUpstreamUnavailable, ""},
		{"429_transient", 429, nil, ErrUpstreamUnavailable, ""},
		{"4xx_permanent", 403, nil, ErrUpstreamPermanent, ""},
		{"404_permanent_by_default", 404, nil, ErrUpstreamPermanent, ""},
		{"404_mapped_to_empty_body", 404, []byte(`{"total":0}`), nil, `{"total":0}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := fetchTestServer(t, c.status, "upstream says no", nil)
			body, err := FetchUpstream(context.Background(), srv.Client(), srv.URL,
				FetchSpec{Prefix: "demo", NotFoundBody: c.notFoundBody})
			if c.wantErr != nil {
				if !errors.Is(err, c.wantErr) {
					t.Fatalf("err = %v, want %v", err, c.wantErr)
				}
				if !strings.HasPrefix(err.Error(), "demo: ") {
					t.Errorf("err %q lacks the demo: prefix", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("FetchUpstream: %v", err)
			}
			if string(body) != c.wantBody {
				t.Errorf("body = %q, want %q", body, c.wantBody)
			}
		})
	}
}

func TestFetchUpstreamTransportError(t *testing.T) {
	srv := fetchTestServer(t, http.StatusOK, "", nil)
	url := srv.URL
	srv.Close() // refuse connections
	_, err := FetchUpstream(context.Background(), http.DefaultClient, url, FetchSpec{Prefix: "demo"})
	if !errors.Is(err, ErrUpstreamUnavailable) {
		t.Errorf("transport error = %v, want ErrUpstreamUnavailable", err)
	}
}

func TestFetchUpstreamNilClientUsesContext(t *testing.T) {
	srv := fetchTestServer(t, http.StatusOK, "ok", nil)
	ctx := WithHTTPClient(context.Background(), srv.Client())
	body, err := FetchUpstream(ctx, nil, srv.URL, FetchSpec{Prefix: "demo"})
	if err != nil || string(body) != "ok" {
		t.Errorf("FetchUpstream(nil client) = (%q, %v), want (ok, nil)", body, err)
	}
}

// zeroReader yields an endless stream of zero bytes without allocating.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// TestFetchUpstreamOverLimit verifies a response larger than the response-size
// bound is rejected (as a permanent failure) rather than read into memory.
func TestFetchUpstreamOverLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.CopyN(w, zeroReader{}, httpx.DefaultMaxResponseBytes+64)
	}))
	defer srv.Close()

	_, err := FetchUpstream(context.Background(), srv.Client(), srv.URL, FetchSpec{Prefix: "test"})
	if err == nil {
		t.Fatal("expected an error for an over-limit response, got nil")
	}
	if !errors.Is(err, ErrUpstreamPermanent) {
		t.Errorf("err = %v, want ErrUpstreamPermanent (over-limit)", err)
	}
}
