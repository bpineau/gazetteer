package georisques

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
//   - HTTP 5xx / transport / parse failure → gazetteer.ErrUpstreamUnavailable (wrapped)
//   - HTTP 4xx (other than 404) → gazetteer.ErrUpstreamPermanent (wrapped)
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

	if l.Address == "" && l.City == "" && l.Zip == "" && !hasCoords(l) {
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

// fetch performs the HTTP GET and translates transport / status-code
// failures to gazetteer sentinels.
func (s *Source) fetch(ctx context.Context, u string) ([]byte, error) {
	client := s.opts.HTTPClient
	if client == nil {
		client = gazetteer.HTTPClientFrom(ctx)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("georisques: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("georisques: %w: %w", gazetteer.ErrUpstreamUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("georisques: %w: http %d", gazetteer.ErrUpstreamUnavailable, resp.StatusCode)
	}
	if resp.StatusCode == http.StatusNotFound {
		// 404 = no rapport for these coords. BRGM normally returns 200
		// + empty body for that case, but be defensive: treat 404 as
		// `{}` so the parser yields a zero-valued Report. The Report
		// path is the canonical empty signal; 404 here is rare.
		return []byte(`{}`), nil
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("georisques: %w: http %d", gazetteer.ErrUpstreamPermanent, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("georisques: %w: read body: %w", gazetteer.ErrUpstreamUnavailable, err)
	}
	return body, nil
}

// resolveLatLon returns (lat, lon) for the listing. Preference order:
//
//  1. Listing.Lat/Lon when both pointers are non-nil and not (0,0).
//  2. The Geocoder's result (when configured).
//  3. An error otherwise.
func (s *Source) resolveLatLon(ctx context.Context, l gazetteer.Listing) (float64, float64, error) {
	if l.Lat != nil && l.Lon != nil && (*l.Lat != 0 || *l.Lon != 0) {
		return *l.Lat, *l.Lon, nil
	}
	if s.opts.Geocoder == nil {
		return 0, 0, errors.New("georisques: lat/lon not resolvable (no geocoder configured)")
	}
	q := banx.GeocodeQuery{
		Address: strings.TrimSpace(l.Address + " " + l.Zip + " " + l.City),
		City:    l.City,
		Zip:     l.Zip,
	}
	res, err := s.opts.Geocoder.Geocode(ctx, q)
	if err != nil {
		return 0, 0, err
	}
	if res.Lat == 0 && res.Lon == 0 {
		return 0, 0, errors.New("georisques: geocoder returned zero coords")
	}
	return res.Lat, res.Lon, nil
}

// hasCoords reports whether the listing carries a non-zero (lat, lon)
// pair via its pointer fields.
func hasCoords(l gazetteer.Listing) bool {
	return l.Lat != nil && l.Lon != nil && (*l.Lat != 0 || *l.Lon != 0)
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
		return nil, errors.New("georisques: typed result mismatch")
	}
	return res, nil
}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
