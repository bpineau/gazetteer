package bdnb

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
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "bdnb"

// sourceVersion bumps when the Source's internal logic changes.
// Stateful callers gate cache invalidation on it.
//
// Version 2 incorporates the residence-prefix leak fix in
// fraddr.Parse + IlikePatternFor.
const sourceVersion = 2

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Options configures a bdnb Source. The zero value is usable: every
// field has a sane default (BaseURL → package var BaseURL; Geocoder →
// nil means the Source cannot resolve INSEE and will return
// ErrInsufficientInputs unless the Listing carries a usable INSEE;
// HTTPClient → gazetteer.HTTPClientFrom(ctx) at Query time).
type Options struct {
	// BaseURL overrides the BDNB PostgREST endpoint. Tests use this to
	// point at httptest.NewServer. Default: package-level BaseURL var.
	BaseURL string

	// Geocoder is consulted to resolve the listing's address into a
	// 5-digit INSEE — required by BDNB's PostgREST filter to avoid
	// 57014 timeouts. Mandatory in practice unless the Listing already
	// carries a usable Listing.INSEE. If the Geocoder also implements
	// banx.ReverseGeocoder, the Source uses the standard
	// forward/reverse cascade (banx.INSEEResolver); otherwise
	// forward-only.
	Geocoder banx.Geocoder

	// HTTPClient overrides the per-query HTTP client. When nil, the
	// Source uses gazetteer.HTTPClientFrom(ctx).
	HTTPClient *http.Client
}

// Source implements gazetteer.Source for the BDNB
// `batiment_groupe_complet` endpoint. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a bdnb Source. Zero-valued Options is fine but the
// Source will return ErrInsufficientInputs on every call unless a
// Geocoder is supplied (BDNB's PostgREST filter requires INSEE).
func NewSource(opts Options) *Source {
	return &Source{opts: opts}
}

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Query implements gazetteer.Source. It resolves the listing's INSEE
// via the BAN cascade, fetches the BDNB row(s) matching that INSEE +
// the listing's address pattern, picks the most likely building, and
// returns a *Result.
//
// Error mapping (the framework translates these to a Result.Status per
// the table in gazetteer/source.go):
//
//   - Missing address+city+zip → gazetteer.ErrInsufficientInputs (wrapped)
//   - Geocoder cannot resolve INSEE → gazetteer.ErrInsufficientInputs (wrapped)
//   - Empty address pattern after fraddr.Parse → gazetteer.ErrInsufficientInputs (wrapped)
//   - HTTP 5xx / transport / parse failure → gazetteer.ErrUpstreamUnavailable (wrapped)
//   - HTTP 4xx (other than 404) → gazetteer.ErrUpstreamPermanent (wrapped)
//   - Successful empty response (rows: []) → (*Result, nil) with
//     IsEmpty()==true; the framework records StatusOKEmpty.
//
// Logging: emits one DEBUG log line per query via
// gazetteer.LoggerFrom(ctx) at the "bdnb" component. Wrappers that
// batch many queries typically log a single INFO line per work-unit.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	logger := gazetteer.LoggerFrom(ctx).With(slog.String("source", Name))

	if l.Address == "" && l.City == "" && l.Zip == "" {
		return nil, fmt.Errorf("bdnb: %w: no address/city/zip", gazetteer.ErrInsufficientInputs)
	}

	insee, inseeSource, err := s.resolveINSEE(ctx, l)
	if err != nil {
		return nil, fmt.Errorf("bdnb: %w: %w", gazetteer.ErrInsufficientInputs, err)
	}

	parts := ParseAddress(l.Address)
	pattern := IlikePatternFor(parts)
	if pattern == "" {
		logger.Debug("bdnb.empty_pattern",
			slog.String("addr", l.Address),
			slog.String("insee", insee),
		)
		return nil, fmt.Errorf("bdnb: %w: no street tokens available for ilike pattern", gazetteer.ErrInsufficientInputs)
	}

	u, err := URLForAddress(insee, pattern)
	if err != nil {
		return nil, fmt.Errorf("bdnb: build url: %w", err)
	}

	// emptyEvidence pre-fills the sidecar fields known before the row
	// pick. RawCount and PickedIndex are overwritten on every return
	// path.
	emptyEvidence := func(rawCount int) Evidence {
		return Evidence{
			MatchStrategy:         MatchByAddressILike,
			INSEE:                 insee,
			INSEEResolutionSource: inseeSource,
			AddressPattern:        pattern,
			RawCount:              rawCount,
			PickedIndex:           -1,
			URL:                   u,
		}
	}

	body, err := s.fetch(ctx, u)
	if err != nil {
		return nil, err
	}

	rows, err := ParseList(body)
	if err != nil {
		return nil, fmt.Errorf("bdnb: parse: %w: %w", gazetteer.ErrUpstreamUnavailable, err)
	}

	if len(rows) == 0 {
		logger.Debug("bdnb.no_match",
			slog.String("insee", insee),
			slog.String("pattern", pattern),
		)
		return &Result{
			Confidence: ConfidenceLow,
			SampleSize: 0,
			Skipped:    true,
			SkipReason: SkipReasonNoMatch,
			Evidence:   emptyEvidence(0),
		}, nil
	}

	// Pick the row whose street number matches (when we have one).
	// Fall back to the most-complete row when no match.
	idx := -1
	if parts.Number != "" {
		if i, ok := PickBestByNumber(rows, parts.Number); ok {
			idx = i
		}
	}
	if idx == -1 {
		i, ok := PickBest(rows, "")
		if !ok {
			logger.Debug("bdnb.no_match",
				slog.String("insee", insee),
				slog.String("pattern", pattern),
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
			logger.Debug("bdnb.number_fallback",
				slog.String("number", parts.Number),
				slog.Int("raw_count", len(rows)),
			)
		}
	}
	row := rows[idx]

	out := BuildResult(row)
	out.SampleSize = 1
	out.Confidence = PickConfidence(true, false, row.FiabiliteCRAdrNiv1)
	out.Evidence = Evidence{
		MatchStrategy:         MatchByAddressILike,
		INSEE:                 insee,
		INSEEResolutionSource: inseeSource,
		AddressPattern:        pattern,
		RawCount:              len(rows),
		PickedIndex:           idx,
		URL:                   u,
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
		return nil, fmt.Errorf("bdnb: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bdnb: %w: %w", gazetteer.ErrUpstreamUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("bdnb: %w: http %d", gazetteer.ErrUpstreamUnavailable, resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound {
		// 404 = no record. PostgREST normally returns 200 + empty []
		// for that case, but be defensive: treat 404 as empty body so
		// the parser path returns (rows: [], nil) and we render the
		// SkipReasonNoMatch sentinel. The ParseList path is the
		// canonical empty signal; 404 here is rare.
		return []byte(`[]`), nil
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("bdnb: %w: http %d", gazetteer.ErrUpstreamPermanent, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("bdnb: %w: read body: %w", gazetteer.ErrUpstreamUnavailable, err)
	}
	return body, nil
}

// resolveINSEE returns the 5-digit INSEE for the listing via the
// canonical BAN cascade (`helpers/banx/insee_resolver.go`):
//
//  1. BAN forward on the free-form address — used only when
//     score ≥ 0.7 AND citycode is non-empty. The score gate is the bug
//     fix: prior to this, BDNB silently accepted any positive BAN match
//     (audit #124: 41 enrichments resolved to wrong departements, e.g.
//     zip 22680 Etables-sur-Mer → 75117 Paris on a 0.32-score match).
//  2. BAN reverse on listing.lat/lon when the structured coords are
//     present — by construction in the correct commune, independent of
//     address-text matching.
//  3. ErrNoINSEE / ErrInsufficientInputs when neither produces an INSEE.
//
// Returns (insee, source) where source ∈ {"ban_forward", "ban_reverse"}
// for traceability in Evidence.INSEEResolutionSource.
func (s *Source) resolveINSEE(ctx context.Context, l gazetteer.Listing) (insee, source string, err error) {
	// If the listing already carries a usable INSEE, trust it.
	if i := strings.TrimSpace(l.INSEE); i != "" {
		return i, "listing", nil
	}
	if s.opts.Geocoder == nil {
		return "", "", errors.New("bdnb: no geocoder configured")
	}

	var auctionLat, auctionLon float64
	if l.Lat != nil {
		auctionLat = *l.Lat
	}
	if l.Lon != nil {
		auctionLon = *l.Lon
	}
	hasText := l.Address != "" || l.City != "" || l.Zip != ""
	hasCoords := auctionLat != 0 && auctionLon != 0
	if !hasText && !hasCoords {
		return "", "", errors.New("bdnb: no address/city/zip + no coords")
	}

	// The Geocoder dependency is the BAN forward client (potentially
	// wrapped by CachedGeocoder). For reverse we accept any geocoder
	// that also implements ReverseGeocoder; otherwise we skip step 2.
	var reverseGC banx.ReverseGeocoder
	if rev, ok := s.opts.Geocoder.(banx.ReverseGeocoder); ok {
		reverseGC = rev
	}

	resolver := &banx.INSEEResolver{
		Forward: s.opts.Geocoder,
		Reverse: reverseGC,
	}
	res, rerr := resolver.Resolve(ctx, banx.INSEEQuery{
		Address: strings.TrimSpace(l.Address + " " + l.Zip + " " + l.City),
		City:    l.City,
		Zip:     l.Zip,
		Lat:     auctionLat,
		Lon:     auctionLon,
	})
	if rerr != nil {
		return "", "", rerr
	}
	if res.INSEE == "" {
		return "", "", errors.New("bdnb: no INSEE resolved")
	}
	return res.INSEE, res.Source, nil
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
		return nil, errors.New("bdnb: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
