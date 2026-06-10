package education

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
const Name = "education"

// sourceVersion bumps when the Source's internal logic changes.
//
// History:
//   - v1: initial. One Opendatasoft GET per Listing, grouped by
//     type_etablissement, filtered on open establishments.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Options configures an education Source. The zero value is usable:
// BaseURL defaults to DefaultBaseURL, HTTPClient defaults to
// gazetteer.HTTPClientFrom(ctx) at Query time.
type Options struct {
	// BaseURL overrides the Opendatasoft records endpoint. Tests use
	// this to point at httptest.NewServer. Default: DefaultBaseURL.
	BaseURL string

	// HTTPClient overrides the per-query HTTP client. When nil, the
	// Source uses gazetteer.HTTPClientFrom(ctx).
	HTTPClient *http.Client

	// Fetcher, when non-nil, replaces the built-in HTTP fetch path for
	// every upstream GET — the seam for injecting circuit breakers, quota
	// trippers or recorded fixtures (helpers/circuit.HTTPFetcher implements
	// it). NOTE: an injected Fetcher takes over the whole fetch contract,
	// including this source's 404→empty-payload default (the Opendatasoft
	// no-rows envelope `{"total_count":0,"results":[]}`) and the Accept
	// header — see gazetteer.Fetcher for the full contract.
	Fetcher gazetteer.Fetcher
}

// Source implements gazetteer.Source for the Annuaire de l'Éducation
// Nationale. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds an education Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source {
	return &Source{opts: opts}
}

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Query implements gazetteer.Source. Pipeline:
//
//  1. Require Listing.INSEE — the upstream API filters by
//     `code_commune`. Without INSEE, return
//     gazetteer.ErrInsufficientInputs.
//  2. Build the records URL (group_by type_etablissement, filtered
//     on OUVERT establishments) and GET it.
//  3. Parse the JSON envelope and project counts onto the typed
//     Result.
//
// Error mapping (framework translates these to Result.Status per
// gazetteer/source.go):
//
//   - Missing INSEE → ErrInsufficientInputs (wrapped)
//   - HTTP 5xx / 429, network, json decode → ErrUpstreamUnavailable
//   - HTTP 4xx (other than 404 / 429) → ErrUpstreamPermanent
//   - HTTP 404 → empty Result (the API normally returns 200+empty)
//
// Logging: one DEBUG line per query via
// gazetteer.LoggerFrom(ctx) at the "education" component.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	logger := gazetteer.LoggerFrom(ctx).With(slog.String("source", Name))

	insee := strings.TrimSpace(l.INSEE)
	if insee == "" {
		return nil, fmt.Errorf("education: %w: listing.INSEE required", gazetteer.ErrInsufficientInputs)
	}

	base := s.opts.BaseURL
	if base == "" {
		base = DefaultBaseURL
	}
	u, err := URLForINSEE(base, insee)
	if err != nil {
		return nil, fmt.Errorf("education: %w: %w", gazetteer.ErrInsufficientInputs, err)
	}

	logger.Debug("education.query",
		slog.String("insee", insee),
		slog.String("url", u),
	)

	body, err := s.fetch(ctx, u)
	if err != nil {
		return nil, err
	}

	res, err := Parse(body)
	if err != nil {
		return nil, fmt.Errorf("education: parse: %w: %w", gazetteer.ErrUpstreamUnavailable, err)
	}

	res.Evidence = Evidence{
		INSEE: insee,
		URL:   u,
	}
	if res.NbTotal > 0 {
		res.Confidence = ConfidenceHigh
	} else {
		res.Confidence = ConfidenceNone
	}
	return res, nil
}

// fetch performs the HTTP GET via the shared gazetteer.FetchUpstream
// helper. The Opendatasoft API normally responds 200 + empty results
// for an unknown commune; a rare 404 is defensively mapped onto the
// same "no rows" envelope so consumers see an empty Result rather than
// a failure.
func (s *Source) fetch(ctx context.Context, u string) ([]byte, error) {
	if s.opts.Fetcher != nil {
		return s.opts.Fetcher.Fetch(ctx, u)
	}
	return gazetteer.FetchUpstream(ctx, s.opts.HTTPClient, u, gazetteer.FetchSpec{
		Prefix:       Name,
		Accept:       "application/json",
		NotFoundBody: []byte(`{"total_count":0,"results":[]}`),
	})
}

// Query is the atomic helper for callers who don't want the builder.
func Query(ctx context.Context, opts Options, l gazetteer.Listing) (*Result, error) {
	return gazetteer.QueryTyped[*Result](ctx, NewSource(opts), l)
}

// QueryResult is Query with the package's typed Result — for callers
// holding a constructed Source instance. Equivalent to the package-level
// Query helper without rebuilding the Source per call.
func (s *Source) QueryResult(ctx context.Context, l gazetteer.Listing) (*Result, error) {
	return gazetteer.QueryTyped[*Result](ctx, s, l)
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
