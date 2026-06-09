package dpedist

import (
	"context"
	"fmt"
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
//   - HTTP 5xx / 429, network, json decode → ErrUpstreamUnavailable
//   - HTTP 4xx (other than 404 / 429) → ErrUpstreamPermanent
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

// fetch performs the HTTP GET via the shared gazetteer.FetchUpstream
// helper. The values_agg API normally responds 200 + zero-totals for
// an unknown commune; a rare 404 is defensively mapped onto the same
// "no rows" envelope so consumers see an empty Result rather than a
// failure. 429 maps to ErrUpstreamUnavailable (retryable).
func (s *Source) fetch(ctx context.Context, u string) ([]byte, error) {
	return gazetteer.FetchUpstream(ctx, s.opts.HTTPClient, u, gazetteer.FetchSpec{
		Prefix:       Name,
		Accept:       "application/json",
		NotFoundBody: []byte(`{"total":0,"total_other":0,"aggs":[]}`),
	})
}

// Query is the atomic helper for callers who don't want the builder.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	return gazetteer.QueryTyped[*Result](ctx, NewSource(opts), l)
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
