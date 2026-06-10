package ademe

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
	"github.com/bpineau/gazetteer/helpers/fraddr"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "ademe"

// sourceVersion bumps when the Source's internal logic changes.
// Stateful callers gate cache invalidation on it.
//
// v3: street-aware matching. Among rows matching the listing's house
// number, those on the same voie (street type word + name tokens, e.g.
// "rue petites ecuries" vs "cour petites ecuries") are preferred, and
// only a number+street+DPE match earns "high" confidence — a
// number-matched but wrong-street row is no longer a false-positive
// "high".
const sourceVersion = 3

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Options configures an ademe Source. The zero value is usable: every
// field has a sane default (BaseURL → DefaultBaseURL; Geocoder → BAN
// hitting the public api-adresse.data.gouv.fr endpoint; HTTPClient →
// gazetteer.HTTPClientFrom(ctx) at Query time).
type Options struct {
	// BaseURL overrides the ADEME data-fair endpoint. Tests use this to
	// point at httptest.NewServer. Default: DefaultBaseURL.
	BaseURL string

	// Geocoder is consulted to resolve a 5-digit FR zip when
	// Listing.Zip is missing / malformed. When nil, the Source uses a
	// banx.BANClient built from the per-query HTTP client (read from
	// ctx via gazetteer.HTTPClientFrom). Tests inject their own
	// implementation.
	Geocoder banx.Geocoder

	// HTTPClient overrides the per-query HTTP client. When nil, the
	// Source uses gazetteer.HTTPClientFrom(ctx).
	HTTPClient *http.Client

	// Fetcher, when non-nil, replaces the built-in HTTP fetch path for
	// every upstream GET — the seam for injecting circuit breakers, quota
	// trippers or recorded fixtures (helpers/circuit.HTTPFetcher implements
	// it). NOTE: an injected Fetcher takes over the whole fetch contract,
	// including this source's 404→empty-payload default (the data-fair
	// empty envelope `{"total":0,"results":[]}`) and the Accept header —
	// see gazetteer.Fetcher for the full contract.
	Fetcher gazetteer.Fetcher
}

// Source implements gazetteer.Source for the ADEME `dpe03existant`
// dataset. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds an ademe Source. Zero-valued Options is fine.
func NewSource(opts Options) *Source {
	return &Source{opts: opts}
}

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Query implements gazetteer.Source. It fetches the listing's DPE from
// the ADEME data-fair endpoint and returns a *Result.
//
// Error mapping (the framework translates these to a Result.Status per
// the table in gazetteer/source.go):
//
//   - Missing address / unresolvable zip / empty query →
//     gazetteer.ErrInsufficientInputs (wrapped)
//   - HTTP 5xx / 429, network, parse failure → gazetteer.ErrUpstreamUnavailable (wrapped)
//   - HTTP 4xx (other than 404 / 429) → gazetteer.ErrUpstreamPermanent (wrapped)
//   - Successful empty response (results: []) → (*Result, nil) with
//     IsEmpty()==true; the framework records StatusOKEmpty.
//
// Logging: emits one DEBUG log line per query via
// gazetteer.LoggerFrom(ctx) at the "ademe" component. Wrappers that
// batch many queries typically log a single INFO line per work-unit.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	logger := gazetteer.LoggerFrom(ctx).With(slog.String("source", Name))

	if l.Address == "" && l.City == "" && l.Zip == "" {
		return nil, fmt.Errorf("ademe: %w: no address/city/zip", gazetteer.ErrInsufficientInputs)
	}

	resolvedZip, err := s.resolveZip(ctx, l)
	if err != nil {
		return nil, fmt.Errorf("ademe: %w: %w", gazetteer.ErrInsufficientInputs, err)
	}

	parts := fraddr.Parse(l.Address)
	query := parts.Query()
	if query == "" {
		logger.Debug("ademe.empty_query",
			slog.String("addr", l.Address),
			slog.String("zip", resolvedZip),
		)
		return nil, fmt.Errorf("ademe: %w: no street tokens available for full-text query", gazetteer.ErrInsufficientInputs)
	}

	u, err := URLForAddress(s.opts.BaseURL, resolvedZip, query)
	if err != nil {
		return nil, fmt.Errorf("ademe: build url: %w", err)
	}

	// emptyEvidence pre-fills the sidecar fields known before the row
	// pick (URL, zip, query). RawCount and PickedIndex are overwritten
	// below on every return path.
	emptyEvidence := func(rawCount int) Evidence {
		return Evidence{
			MatchStrategy: MatchByZipFulltext,
			Zip:           resolvedZip,
			Query:         query,
			RawCount:      rawCount,
			PickedIndex:   -1,
			NumberMatched: false,
			URL:           u,
		}
	}

	body, err := s.fetch(ctx, u)
	if err != nil {
		return nil, err
	}

	rows, err := ParseList(body)
	if err != nil {
		return nil, fmt.Errorf("ademe: parse: %w: %w", gazetteer.ErrUpstreamUnavailable, err)
	}

	if len(rows) == 0 {
		logger.Debug("ademe.no_match",
			slog.String("zip", resolvedZip),
			slog.String("query", query),
		)
		return &Result{
			Confidence: ConfidenceLow,
			SampleSize: 0,
			Skipped:    true,
			SkipReason: SkipReasonNoMatch,
			Evidence:   emptyEvidence(0),
		}, nil
	}

	// Pick the row whose adresse starts with the listing's number when
	// available; otherwise fall back to PickBest. Both pickers honour
	// the listing's surface (when supplied) to disambiguate between
	// dwellings at the same street number — apartment buildings carry
	// one DPE row per logement and the right answer is the one whose
	// surface is closest to what the caller actually owns.
	wantSurface := 0.0
	if l.SurfaceM2 != nil {
		wantSurface = *l.SurfaceM2
	}
	// wantStreetKey is the listing's street signature (type word + name
	// tokens) — the discriminator that tells "Rue des Petites Ecuries"
	// apart from "Cour des Petites Ecuries", which fraddr deliberately
	// cannot (it drops the type word). Empty ⇒ no usable street ⇒
	// street-matching is treated as "unknown", never as a mismatch.
	wantStreetKey := streetKey(l.Address)
	idx := -1
	numberMatched := false
	if parts.Number != "" {
		if i, ok := PickBestByNumber(rows, parts.Number, wantStreetKey, wantSurface); ok {
			idx = i
			numberMatched = true
		}
	}
	if idx == -1 {
		i, ok := PickBest(rows, wantSurface)
		if !ok {
			logger.Debug("ademe.no_match",
				slog.String("zip", resolvedZip),
				slog.String("query", query),
				slog.Int("raw_count", len(rows)),
			)
			return &Result{
				Confidence: ConfidenceLow,
				SampleSize: 0,
				Skipped:    true,
				SkipReason: SkipReasonNoMatch,
				Evidence:   emptyEvidence(len(rows)),
			}, nil
		}
		idx = i
		if parts.Number != "" {
			logger.Debug("ademe.number_fallback",
				slog.String("number", parts.Number),
				slog.Int("raw_count", len(rows)),
			)
		}
	}
	row := rows[idx]

	// streetMatched is fully derivable from the finally-picked row, so
	// compute it once here — covering both the number-match and full-text
	// fallback paths — rather than threading it out of the picker.
	streetMatched := streetMatches(wantStreetKey, row)

	out := buildResult(row)
	out.SampleSize = 1
	out.Confidence = PickConfidence(true, numberMatched, streetMatched, row.EtiquetteDPE)
	out.Evidence = Evidence{
		MatchStrategy: MatchByZipFulltext,
		Zip:           resolvedZip,
		Query:         query,
		RawCount:      len(rows),
		PickedIndex:   idx,
		NumberMatched: numberMatched,
		StreetMatched: streetMatched,
		URL:           u,
	}
	return out, nil
}

// fetch performs the HTTP GET via the shared gazetteer.FetchUpstream
// helper. 404 = no record: data-fair normally returns 200 + empty
// results for that case, but be defensive and map a rare 404 onto the
// same canonical empty envelope the ParseList path expects.
func (s *Source) fetch(ctx context.Context, u string) ([]byte, error) {
	if s.opts.Fetcher != nil {
		return s.opts.Fetcher.Fetch(ctx, u)
	}
	return gazetteer.FetchUpstream(ctx, s.opts.HTTPClient, u, gazetteer.FetchSpec{
		Prefix:       Name,
		Accept:       "application/json",
		NotFoundBody: []byte(`{"total":0,"results":[]}`),
	})
}

// resolveZip returns the 5-digit zip for the listing. Preference order:
//
//  1. Listing.Zip if it looks like a 5-digit FR zip.
//  2. The Geocoder's PostCode (when set / on the Options).
//  3. An error otherwise.
func (s *Source) resolveZip(ctx context.Context, l gazetteer.Listing) (string, error) {
	if z := strings.TrimSpace(l.Zip); fraddr.IsFrPostalCode(z) {
		return z, nil
	}
	geocoder := s.opts.Geocoder
	if geocoder == nil {
		return "", errors.New("ademe: zip not resolvable (no geocoder configured)")
	}
	q := banx.GeocodeQuery{
		Address: strings.TrimSpace(l.Address + " " + l.Zip + " " + l.City),
		City:    l.City,
		Zip:     l.Zip,
	}
	res, err := geocoder.Geocode(ctx, q)
	if err != nil {
		return "", err
	}
	if fraddr.IsFrPostalCode(res.PostCode) {
		return res.PostCode, nil
	}
	return "", errors.New("ademe: zip not resolvable (geocoder returned no postcode)")
}

// Query is the atomic helper for callers who don't want the builder.
// The error is non-nil only when the Source failed; a successful but
// empty response still returns a non-nil *Result with IsEmpty() == true.
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
