package ademe

import (
	"context"
	"errors"
	"fmt"
	"io"
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
const sourceVersion = 2

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
//   - HTTP 5xx, network, parse failure → gazetteer.ErrUpstreamUnavailable (wrapped)
//   - HTTP 4xx (other than 404) → gazetteer.ErrUpstreamPermanent (wrapped)
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
	idx := -1
	numberMatched := false
	if parts.Number != "" {
		if i, ok := PickBestByNumber(rows, parts.Number, wantSurface); ok {
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

	out := buildResult(row)
	out.SampleSize = 1
	out.Confidence = PickConfidence(true, numberMatched, row.EtiquetteDPE)
	out.Evidence = Evidence{
		MatchStrategy: MatchByZipFulltext,
		Zip:           resolvedZip,
		Query:         query,
		RawCount:      len(rows),
		PickedIndex:   idx,
		NumberMatched: numberMatched,
		URL:           u,
	}
	return out, nil
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
		return nil, fmt.Errorf("ademe: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ademe: %w: %w", gazetteer.ErrUpstreamUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("ademe: %w: http %d", gazetteer.ErrUpstreamUnavailable, resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound {
		// 404 = no record. data-fair normally returns 200 + empty
		// results for that case, but be defensive: treat 404 as
		// empty body so the parser returns ErrEmptyBody → upstream
		// transient. The ParseList path is the canonical empty
		// signal; 404 here is rare.
		return []byte(`{"total":0,"results":[]}`), nil
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("ademe: %w: http %d", gazetteer.ErrUpstreamPermanent, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("ademe: %w: read body: %w", gazetteer.ErrUpstreamUnavailable, err)
	}
	return body, nil
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
	data, err := NewSource(opts).Query(ctx, l)
	if err != nil {
		return nil, err
	}
	res, ok := data.(*Result)
	if !ok {
		return nil, errors.New("ademe: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
