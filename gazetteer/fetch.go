package gazetteer

import (
	"context"
	"fmt"
	"io"
	"net/http"

	"github.com/bpineau/gazetteer/helpers/httpx"
)

// Fetcher is the injectable fetch seam every live-HTTP Source exposes on
// its Options (conventionally as an `Fetcher gazetteer.Fetcher` field,
// consulted before the built-in FetchUpstream path). Injecting one lets a
// caller put its own machinery between the Source and the network —
// circuit breakers, quota trippers, request mirroring, recorded fixtures
// — while keeping the Source's URL building and response parsing.
// helpers/circuit.HTTPFetcher and helpers/circuit.FuncFetcher implement
// it.
//
// Contract for implementations:
//   - Return the response body for 2xx answers.
//   - Map failures onto the gazetteer error taxonomy (wrap
//     ErrUpstreamUnavailable / ErrUpstreamPermanent / ErrAntiBot /
//     a CircuitTrippedError) when Status classification matters to you;
//     unwrapped errors classify as StatusFailedTransient.
//   - An injected Fetcher fully replaces FetchUpstream, including each
//     Source's 404→empty-payload mapping (see the Source's fetch godoc
//     for its default NotFoundBody) — handle 404 accordingly.
type Fetcher interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
}

// FetchSpec configures FetchUpstream. The zero value of every field is
// meaningful: no Accept header, and 404 treated as a permanent failure.
type FetchSpec struct {
	// Prefix prefixes every error message, conventionally the Source name
	// (optionally with a sub-endpoint, e.g. "cadastre: bati").
	Prefix string

	// Accept sets the Accept request header when non-empty.
	Accept string

	// NotFoundBody, when non-nil, is returned verbatim for a 404 response
	// instead of an error — for APIs where 404 means "no data here" (the
	// caller supplies the empty payload its parser expects). When nil, 404
	// maps to ErrUpstreamPermanent like any other 4xx.
	NotFoundBody []byte
}

// FetchUpstream performs an HTTP GET against url and translates transport
// and status-code failures into the gazetteer error taxonomy:
//
//   - transport error, 5xx, 429  → ErrUpstreamUnavailable (transient)
//   - 404                        → spec.NotFoundBody when non-nil, else
//     ErrUpstreamPermanent
//   - other 4xx                  → ErrUpstreamPermanent
//
// A nil client falls back to HTTPClientFrom(ctx) — the standard per-Source
// precedence (Options.HTTPClient first, Builder-propagated client second).
// This is the shared body of every live-HTTP source's fetch helper; only
// the Accept header and the 404 policy vary per upstream.
func FetchUpstream(ctx context.Context, client *http.Client, url string, spec FetchSpec) ([]byte, error) {
	if client == nil {
		client = HTTPClientFrom(ctx)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("%s: build request: %w", spec.Prefix, err)
	}
	if spec.Accept != "" {
		req.Header.Set("Accept", spec.Accept)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w: %w", spec.Prefix, ErrUpstreamUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotFound && spec.NotFoundBody != nil:
		return spec.NotFoundBody, nil
	case resp.StatusCode >= 500, resp.StatusCode == http.StatusTooManyRequests:
		return nil, fmt.Errorf("%s: %w: http %d", spec.Prefix, ErrUpstreamUnavailable, resp.StatusCode)
	case resp.StatusCode >= 400:
		return nil, fmt.Errorf("%s: %w: http %d", spec.Prefix, ErrUpstreamPermanent, resp.StatusCode)
	}

	// Bound the read so a runaway or malicious response cannot exhaust memory,
	// mirroring httpx.DefaultMaxResponseBytes. LimitReader caps at limit+1 so an
	// over-limit body is detected (len > limit) rather than silently truncated.
	const limit = httpx.DefaultMaxResponseBytes
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%s: %w: read body: %w", spec.Prefix, ErrUpstreamUnavailable, err)
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s: %w: response exceeds %d bytes", spec.Prefix, ErrUpstreamPermanent, limit)
	}
	return body, nil
}
