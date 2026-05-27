package dpedist

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "dpedist"

// sourceVersion bumps when the Source's internal logic changes.
//
// History:
//   - v1: initial. One data-fair `values_agg` GET per Listing,
//     grouped by etiquette_dpe, surfaced as A..G counts + shares +
//     headline F+G passoire share.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Options configures a dpedist Source. The zero value is usable:
// BaseURL defaults to DefaultBaseURL, HTTPClient defaults to
// gazetteer.HTTPClientFrom(ctx) at Query time.
type Options struct {
	// BaseURL overrides the data-fair values_agg endpoint. Tests use
	// this to point at httptest.NewServer. Default: DefaultBaseURL.
	BaseURL string

	// HTTPClient overrides the per-query HTTP client. When nil, the
	// Source uses gazetteer.HTTPClientFrom(ctx).
	HTTPClient *http.Client
}

// Source implements gazetteer.Source for the ADEME DPE distribution
// per commune. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a dpedist Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source {
	return &Source{opts: opts}
}

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// BaseURL implements gazetteer.BaseURLer. Returns the effective
// upstream root the Source will hit.
func (s *Source) BaseURL() string {
	if s.opts.BaseURL != "" {
		return s.opts.BaseURL
	}
	return DefaultBaseURL
}

// Query implements gazetteer.Source. Pipeline:
//
//  1. Require Listing.INSEE — the upstream filters by `code_insee_ban`.
//     Without INSEE, return gazetteer.ErrInsufficientInputs.
//  2. Build the values_agg URL (field=etiquette_dpe, qs=insee, size=0)
//     and GET it.
//  3. Parse the JSON envelope, fold the buckets onto the A..G + N
//     enum, compute shares + the F+G passoire headline.
//
// Error mapping (framework translates these to Result.Status per
// gazetteer/source.go):
//
//   - Missing INSEE → ErrInsufficientInputs (wrapped)
//   - HTTP 5xx, network, json decode → ErrUpstreamUnavailable
//   - HTTP 4xx (other than 404) → ErrUpstreamPermanent
//   - HTTP 404 → empty Result (the API normally returns 200 with
//     total=0 for an unknown commune)
//
// Logging: one DEBUG line per query via gazetteer.LoggerFrom(ctx)
// at the "dpedist" component.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	logger := gazetteer.LoggerFrom(ctx).With(slog.String("source", Name))

	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("dpedist: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}

	base := s.opts.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	u, err := URLForINSEE(base, insee)
	if err != nil {
		return nil, fmt.Errorf("dpedist: %w: %w", gazetteer.ErrInsufficientInputs, err)
	}

	logger.Debug("dpedist.query",
		slog.String("insee", insee),
		slog.String("url", u),
	)

	body, err := s.fetch(ctx, u)
	if err != nil {
		return nil, err
	}

	res, err := Parse(body)
	if err != nil {
		return nil, fmt.Errorf("dpedist: parse: %w: %w", gazetteer.ErrUpstreamUnavailable, err)
	}

	res.Evidence = Evidence{INSEE: insee, URL: u}
	switch {
	case res.NbTotal == 0:
		res.Confidence = ConfidenceNone
	case res.NbTotal < ThinSampleThreshold:
		res.Confidence = ConfidenceLow
	default:
		res.Confidence = ConfidenceHigh
	}
	return res, nil
}

// fetch performs the HTTP GET and translates transport / status-code
// failures to gazetteer sentinels.
func (s *Source) fetch(ctx context.Context, u string) ([]byte, error) {
	client := s.opts.HTTPClient
	if client == nil {
		client = gazetteer.HTTPClientFrom(ctx)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("dpedist: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dpedist: %w: %w", gazetteer.ErrUpstreamUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("dpedist: %w: http %d", gazetteer.ErrUpstreamUnavailable, resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound {
		// The values_agg API normally responds 200 + zero-totals for
		// an unknown commune ; treat 404 defensively as "no rows" so
		// consumers see an empty Result rather than a transient
		// failure.
		return []byte(`{"total":0,"total_other":0,"aggs":[]}`), nil
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("dpedist: %w: http 429", gazetteer.ErrUpstreamUnavailable)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("dpedist: %w: http %d", gazetteer.ErrUpstreamPermanent, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("dpedist: %w: read body: %w", gazetteer.ErrUpstreamUnavailable, err)
	}
	return body, nil
}

// Query is the atomic helper for callers who don't want the builder.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	data, err := NewSource(opts).Query(ctx, l)
	if err != nil {
		return nil, err
	}
	res, ok := data.(*Result)
	if !ok {
		return nil, errors.New("dpedist: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
