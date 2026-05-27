package httpx

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// Client is the project's single HTTP client. It wraps an *http.Client
// configured with the composite transport (snapshot → cache → retry →
// rate-limit → stdlib) plus per-host headers and the User-Agent.
//
// Construct with New(Options). The zero value is not usable.
type Client struct {
	resolved  resolved
	http      *http.Client
	transport http.RoundTripper
}

// New builds a Client. It returns an error only on obviously-broken
// configurations (e.g. MaxRetries < 0 — currently those are normalised to
// safe defaults so the function never errors in v1, but the signature is
// reserved).
func New(opts Options) (*Client, error) {
	r := opts.resolve()
	rt := composeTransport(r)

	c := &http.Client{
		Transport: rt,
		Jar:       r.cookieJar,
		Timeout:   defaultClientTimeout,
	}

	return &Client{
		resolved:  r,
		http:      c,
		transport: rt,
	}, nil
}

// Transport returns the assembled http.RoundTripper, ready to be plugged
// into a colly.Collector via Collector.WithTransport. Note: when using
// httpx.Transport() with colly, the source code MUST disable colly's
// LimitRule to avoid double-throttling.
func (c *Client) Transport() http.RoundTripper { return c.transport }

// Close releases any pooled connections. Safe to call multiple times.
// Currently mostly here for symmetry — nothing else needs cleanup.
func (c *Client) Close() error {
	if t, ok := c.resolved.innerTransport.(*http.Transport); ok {
		t.CloseIdleConnections()
	}
	return nil
}

// HTTPClient returns the underlying *http.Client. Most callers should
// prefer GetJSON / GetBytes / Download; this exists for the rare cases
// (e.g. multipart POST) the helpers don't cover.
func (c *Client) HTTPClient() *http.Client { return c.http }

// GetJSON performs a GET, decodes the response body as JSON into out, and
// returns an error on transport / non-2xx / decode failure.
func (c *Client) GetJSON(ctx context.Context, url string, hdr http.Header, out any) error {
	body, _, err := c.GetBytes(ctx, url, hdr)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("httpx: decode JSON from %s: %w", url, err)
	}
	return nil
}

// GetBytes performs a GET and returns the body bytes plus a Response
// summary. The caller does not need to close anything (the body has been
// fully read by the time GetBytes returns).
func (c *Client) GetBytes(ctx context.Context, url string, hdr http.Header) ([]byte, *Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("httpx: build request for %s: %w", url, err)
	}
	c.applyDefaultHeaders(req, hdr)

	start := c.resolved.now()
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()

	limit := c.resolved.maxResponseBytes
	var rdr io.Reader = resp.Body
	if limit > 0 {
		rdr = io.LimitReader(resp.Body, limit+1)
	}
	body, err := io.ReadAll(rdr)
	if err != nil {
		return nil, nil, fmt.Errorf("httpx: read body from %s: %w", url, err)
	}
	if limit > 0 && int64(len(body)) > limit {
		return nil, nil, fmt.Errorf("httpx: response from %s exceeds MaxResponseBytes=%d", url, limit)
	}

	r := &Response{
		Status:      resp.StatusCode,
		Header:      resp.Header.Clone(),
		DurationMs:  time.Since(start).Milliseconds(),
		FromCache:   resp.Header.Get("X-From-Cache") == "1",
		Attempts:    1, // detailed retry-attempt count is not threaded through; debug log carries it
		URL:         url,
		ContentType: resp.Header.Get("Content-Type"),
		BodyBytes:   int64(len(body)),
	}

	c.resolved.logger.Debug("request",
		slog.String("method", "GET"),
		slog.String("url", url),
		slog.Int("status", r.Status),
		slog.Int64("duration_ms", r.DurationMs),
		slog.Bool("from_cache", r.FromCache),
		slog.Int64("size_bytes", r.BodyBytes),
	)

	if resp.StatusCode >= 400 {
		snippet := body
		if len(snippet) > defaultErrBodySnippetSize {
			snippet = snippet[:defaultErrBodySnippetSize]
		}
		return body, r, &ErrHTTP{Status: resp.StatusCode, URL: url, Body: snippet}
	}

	return body, r, nil
}

// browserClientHints are the Client Hints / Sec-Fetch / Accept-* headers
// real Chrome 147 sends on a top-level navigation. We attach them to every
// outgoing request unless the caller (or per-host override) already set
// them. Empty values mean "skip this header".
//
// Captured live from the operator's Chrome via `nc -l 4242` (2026-05-02).
// Bump alongside DefaultUserAgent when Chrome rolls a major.
//
// IMPORTANT — Accept-Encoding is intentionally OMITTED from this bundle.
// Go's net/http only auto-decompresses gzip when the user did NOT set
// Accept-Encoding manually. Setting "gzip, deflate, br, zstd" ourselves
// opts us OUT of the auto-decompression and forced 198 locservice parses
// to fail before this fix was applied (regression test
// TestAcceptEncoding_AutoDecompressed). Without this header, the stdlib
// transparently appends `Accept-Encoding: gzip` and decodes the response —
// exactly what every parser in the project wants. The trade-off (we lose
// brotli + zstd negotiation) is acceptable at our volume (< 1 K req/min).
// The bienici enricher keeps its `Accept-Encoding: identity` override
// (cf. a downstream consumer) — strictly more restrictive
// than the default and still compatible.
var browserClientHints = [...]struct {
	name, value string
}{
	{"Accept", "text/html,application/xhtml+xml,application/xml;q=0.9," +
		"image/avif,image/webp,image/apng,*/*;q=0.8," +
		"application/signed-exchange;v=b3;q=0.7"},
	{"Accept-Language", "fr-FR,fr;q=0.9,en-US;q=0.8,en;q=0.7"},
	{"Sec-Ch-Ua", `"Google Chrome";v="147", "Not.A/Brand";v="8", "Chromium";v="147"`},
	{"Sec-Ch-Ua-Mobile", "?0"},
	{"Sec-Ch-Ua-Platform", `"macOS"`},
	{"Sec-Fetch-Site", "none"},
	{"Sec-Fetch-Mode", "navigate"},
	{"Sec-Fetch-User", "?1"},
	{"Sec-Fetch-Dest", "document"},
	{"Upgrade-Insecure-Requests", "1"},
}

// applyDefaultHeaders fills User-Agent + Chrome-mimicking client hints
// and per-host headers, letting any caller-supplied hdr override.
func (c *Client) applyDefaultHeaders(req *http.Request, hdr http.Header) {
	if req.Header == nil {
		req.Header = make(http.Header)
	}

	ua := c.resolved.userAgent
	host := req.URL.Host
	if c.resolved.perHost != nil {
		if h, ok := c.resolved.perHost[host]; ok {
			if h.UserAgent != nil && *h.UserAgent != "" {
				ua = *h.UserAgent
			}
			for k, vs := range h.Headers {
				for _, v := range vs {
					req.Header.Add(k, v)
				}
			}
		}
	}
	req.Header.Set("User-Agent", ua)

	for _, h := range browserClientHints {
		if req.Header.Get(h.name) == "" && h.value != "" {
			req.Header.Set(h.name, h.value)
		}
	}

	for k, vs := range hdr {
		// caller wins
		req.Header.Del(k)
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
}
