package georisques

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/banx"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "georisques"

// sourceVersion bumps when the Source's internal logic changes.
// Stateful callers gate cache invalidation on it.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Options configures a georisques Source. The zero value is usable: every
// field has a sane default (BaseURL → package var BaseURL; Geocoder →
// nil means the Source cannot resolve lat/lon and will return
// ErrInsufficientInputs unless the Listing carries usable lat/lon
// pointers; HTTPClient → gazetteer.HTTPClientFrom(ctx) at Query time).
type Options struct {
	// BaseURL overrides the Georisques rapport-risque endpoint. Tests
	// use this to point at httptest.NewServer. Default: package-level
	// BaseURL var.
	BaseURL string

	// Geocoder is consulted to resolve the listing's address into
	// (lat, lon) when the Listing's Lat/Lon pointers are nil/zero. When
	// nil, the Source falls back to the listing's coords; if neither is
	// usable it returns ErrInsufficientInputs.
	Geocoder banx.Geocoder

	// HTTPClient overrides the per-query HTTP client. When nil, the
	// Source uses gazetteer.HTTPClientFrom(ctx).
	HTTPClient *http.Client

	// Fetcher, when non-nil, replaces the built-in HTTP fetch path for
	// every upstream GET — the seam for injecting circuit breakers, quota
	// trippers or recorded fixtures (helpers/circuit.HTTPFetcher implements
	// it). NOTE: an injected Fetcher takes over the whole fetch contract,
	// including this source's 404→empty-payload default (the empty JSON
	// object `{}`, parsed as a zero-valued Report) and the Accept header —
	// see gazetteer.Fetcher for the full contract.
	Fetcher gazetteer.Fetcher
}

// Source implements gazetteer.Source for the Georisques
// `resultats_rapport_risque` endpoint. Use NewSource to construct.
type Source struct {
	opts Options
}

// NewSource builds a georisques Source. Zero-valued Options is fine but
// the Source will return ErrInsufficientInputs on every call whose
// Listing has neither resolved lat/lon coords nor an address the
// Geocoder can map.
func NewSource(opts Options) *Source {
	return &Source{opts: opts}
}

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// Query implements gazetteer.Source. It resolves the listing's lat/lon
// (preferring the Listing's pointers; falling back to a Geocoder when
// configured), fetches the BRGM rapport-risque, flattens the response,
// and returns a *Result.
//
// Error mapping (the framework translates these to a Result.Status per
// the table in gazetteer/source.go):
//
//   - Missing address+city+zip → gazetteer.ErrInsufficientInputs (wrapped)
//   - Geocoder cannot resolve lat/lon → gazetteer.ErrInsufficientInputs (wrapped)
//   - URL builder rejects coords → gazetteer.ErrInsufficientInputs (wrapped)
//   - HTTP 5xx / 429 / transport / parse failure → gazetteer.ErrUpstreamUnavailable (wrapped)
//   - HTTP 4xx (other than 404 / 429) → gazetteer.ErrUpstreamPermanent (wrapped)
//
// Successful empty parses (Report parsed but Adresse + Commune empty)
// are NOT treated as errors — the Source returns a *Result whose
// IsEmpty()==true and the framework records StatusOKEmpty.
//
// Logging: emits one DEBUG log line per query via
// gazetteer.LoggerFrom(ctx) at the "georisques" component. Wrappers
// that batch many queries typically log a single INFO line per
// work-unit.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	logger := gazetteer.LoggerFrom(ctx).With(slog.String("source", Name))

	if _, _, hasCoords := l.Coords(); l.Address == "" && l.City == "" && l.Zip == "" && !hasCoords {
		return nil, fmt.Errorf("georisques: %w: no address/city/zip/coords", gazetteer.ErrInsufficientInputs)
	}

	lat, lon, err := s.resolveLatLon(ctx, l)
	if err != nil {
		return nil, fmt.Errorf("georisques: %w: %w", gazetteer.ErrInsufficientInputs, err)
	}

	u, err := URLForLatLon(lat, lon)
	if err != nil {
		return nil, fmt.Errorf("georisques: %w: %w", gazetteer.ErrInsufficientInputs, err)
	}
	u = s.applyBaseURL(u)

	body, err := s.fetch(ctx, u)
	if err != nil {
		return nil, err
	}

	report, err := ParseReport(body)
	if err != nil {
		return nil, fmt.Errorf("georisques: parse: %w: %w", gazetteer.ErrUpstreamUnavailable, err)
	}

	out := BuildResult(report)
	out.Evidence = Evidence{
		Lat:       lat,
		Lon:       lon,
		URL:       u,
		LevelUsed: out.LevelUsed,
	}

	if out.LevelUsed == LevelCommune {
		// BRGM downgraded the request to commune scope —
		// `statutAdresse` fields will be empty and red_flags can only
		// surface commune-scale risks. Useful audit signal: payloads
		// whose `level_used == "commune"` despite populated
		// listing.lat/lon often point to BRGM coverage gaps.
		logger.Debug("georisques.commune_fallback",
			slog.Float64("lat", lat),
			slog.Float64("lon", lon),
		)
	}

	return out, nil
}

// applyBaseURL rewrites the leading endpoint root with s.opts.BaseURL
// when set. The URL builder embeds the package-level BaseURL var; this
// method lets tests (and any caller that wires Options.BaseURL) point
// the Source at an httptest.NewServer without mutating package state,
// keeping concurrent tests under -race safe.
func (s *Source) applyBaseURL(u string) string {
	if s.opts.BaseURL == "" {
		return u
	}
	return s.opts.BaseURL + strings.TrimPrefix(u, BaseURL)
}

// fetch performs the HTTP GET via the shared gazetteer.FetchUpstream
// helper. 404 = no rapport for these coords: BRGM normally returns 200
// + empty body for that case, but be defensive and map a rare 404 onto
// `{}` so the parser yields a zero-valued Report — the canonical empty
// signal.
func (s *Source) fetch(ctx context.Context, u string) ([]byte, error) {
	if s.opts.Fetcher != nil {
		return s.opts.Fetcher.Fetch(ctx, u)
	}
	return gazetteer.FetchUpstream(ctx, s.opts.HTTPClient, u, gazetteer.FetchSpec{
		Prefix:       Name,
		Accept:       "application/json",
		NotFoundBody: []byte(`{}`),
	})
}

// resolveLatLon returns (lat, lon) for the listing: the Listing's own
// coordinates when usable (Listing.Coords), else the Geocoder fallback
// via banx.ResolveLatLon.
func (s *Source) resolveLatLon(ctx context.Context, l gazetteer.Listing) (float64, float64, error) {
	if lat, lon, ok := l.Coords(); ok {
		return lat, lon, nil
	}
	lat, lon, err := banx.ResolveLatLon(ctx, s.opts.Geocoder,
		strings.TrimSpace(l.Address+" "+l.Zip+" "+l.City), l.City, l.Zip)
	if err != nil {
		return 0, 0, fmt.Errorf("georisques: %w", err)
	}
	return lat, lon, nil
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
