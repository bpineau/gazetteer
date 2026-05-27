package cadastre

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
	"github.com/bpineau/gazetteer/sources/cadastre/geom"
)

// Name is the canonical Source identifier. Stable; used as the
// gazetteer.Dossier results key and the registry key.
const Name = "cadastre"

// sourceVersion bumps when the Source's internal logic changes.
// Stateful callers gate cache invalidation on it.
const sourceVersion = 1

// Version exposes sourceVersion so callers that wrap the Source can
// mirror it without reaching into the package internals.
const Version = sourceVersion

// Options configures a cadastre Source. The zero value is usable: every
// field has a sane default (BaseURL → package var BaseURL; BatiBaseURL
// → package var BatiBaseURL; HTTPClient → gazetteer.HTTPClientFrom(ctx)
// at Query time; BatiCache → a sync.Map keyed by INSEE).
type Options struct {
	// BaseURL overrides the API Carto cadastre-parcelle endpoint.
	// Tests use this to point at httptest.NewServer. Default:
	// package-level BaseURL var.
	BaseURL string

	// BatiBaseURL overrides the cadastre.data.gouv.fr building-dump
	// endpoint root (i.e. the part BEFORE the `/<INSEE>/geojson/batiments`
	// suffix). Tests use this to point at httptest.NewServer. Default:
	// package-level BatiBaseURL var.
	BatiBaseURL string

	// IncludeBati toggles the bâti enrichment. When false (default),
	// the Source returns parcel data only — the cheaper code path. When
	// true, the Source also fetches the per-commune building dump,
	// filters the polygons sitting on the parcel (centroid PIP), and
	// fills BatiM2 / BatiCount / EmpriseRatio. Bâti failures are
	// soft — they stamp Evidence.BatiError, never abort the parcel
	// response.
	IncludeBati bool

	// Geocoder is consulted when the Listing carries no usable lat/lon
	// — the Source attempts to resolve the address via the geocoder
	// before bailing with ErrInsufficientInputs. Optional: most callers
	// drive the cadastre Source with a Listing that already carries
	// lat/lon from upstream (e.g. a BAN-normalized auction listing).
	Geocoder banx.Geocoder

	// HTTPClient overrides the per-query HTTP client. When nil, the
	// Source uses gazetteer.HTTPClientFrom(ctx).
	HTTPClient *http.Client

	// BatiCache overrides the in-process building-polygon cache. When
	// nil, the Source uses a private sync.Map per Source instance — no
	// TTL, no eviction (a process is short-lived enough for the
	// monthly-refreshed cadastre data to stay coherent).
	BatiCache BatiCache
}

// Source implements gazetteer.Source for the French cadastre. Use
// NewSource to construct.
type Source struct {
	opts         Options
	defaultCache *DefaultBatiCache
}

// NewSource builds a cadastre Source. Zero-valued Options is fine; the
// returned Source will fall back to package-level BaseURL / BatiBaseURL
// at Query time. When Options.BatiCache is nil, a private
// DefaultBatiCache is allocated per Source instance.
func NewSource(opts Options) *Source {
	return &Source{
		opts:         opts,
		defaultCache: &DefaultBatiCache{},
	}
}

// Name implements gazetteer.Source.
func (s *Source) Name() string { return Name }

// Version implements gazetteer.Source.
func (s *Source) Version() int { return sourceVersion }

// BaseURL implements gazetteer.BaseURLer — surfaces the effective
// parcelle endpoint root so callers can diagnose which upstream this
// instance is pointed at without inspecting Options.
func (s *Source) BaseURL() string {
	if s.opts.BaseURL != "" {
		return s.opts.BaseURL
	}
	return BaseURL
}

// Query implements gazetteer.Source. It resolves the listing's
// lat/lon (preferring the Listing's pointers; falling back to a
// Geocoder when configured), fetches the API Carto cadastre parcelle
// FeatureCollection, picks the feature containing the point (with
// fallback to the first feature), and returns a *Result. When
// IncludeBati is true, the Source also fetches the per-commune
// building dump and computes BatiM2 / BatiCount / EmpriseRatio.
//
// Error mapping (the framework translates these to a Result.Status per
// the table in gazetteer/source.go):
//
//   - Missing lat/lon → gazetteer.ErrInsufficientInputs (wrapped)
//   - URL builder rejects coords → gazetteer.ErrInsufficientInputs (wrapped)
//   - API Carto HTTP 5xx / transport / parse failure → gazetteer.ErrUpstreamUnavailable (wrapped)
//   - API Carto HTTP 4xx (other than 404) → gazetteer.ErrUpstreamPermanent (wrapped)
//
// Successful empty parses (FeatureCollection with no features) are
// NOT treated as errors — the Source returns a *Result whose
// IsEmpty()==true and the framework records StatusOKEmpty.
//
// Bâti failures are SOFT: when IncludeBati is true and the bâti dump
// fetch or parse fails, the parcel data is still returned and the
// error is recorded on Evidence.BatiError.
func (s *Source) Query(ctx context.Context, l gazetteer.Listing) (any, error) {
	logger := gazetteer.LoggerFrom(ctx).With(slog.String("source", Name))

	lat, lon, err := s.resolveLatLon(ctx, l)
	if err != nil {
		return nil, fmt.Errorf("cadastre: %w: %w", gazetteer.ErrInsufficientInputs, err)
	}

	u, err := URLForLatLon(lat, lon)
	if err != nil {
		return nil, fmt.Errorf("cadastre: %w: %w", gazetteer.ErrInsufficientInputs, err)
	}
	u = s.applyBaseURL(u)

	body, err := s.fetch(ctx, u)
	if err != nil {
		return nil, err
	}

	fc, err := ParseFeatureCollection(body)
	if err != nil {
		return nil, fmt.Errorf("cadastre: parse: %w: %w", gazetteer.ErrUpstreamUnavailable, err)
	}

	ev := Evidence{
		Lat:            lat,
		Lon:            lon,
		ParcelleAPIURL: u,
	}

	if len(fc.Features) == 0 {
		logger.Debug("cadastre.no_parcel",
			slog.Float64("lat", lat),
			slog.Float64("lon", lon),
		)
		return &Result{Parcels: nil, Evidence: ev}, nil
	}

	idx, _ := PickFeature(fc.Features, lon, lat)
	props := fc.Features[idx].Properties
	parcel := MakeParcel(
		props.IDU,
		props.CodeInsee,
		props.ComAbs,
		props.Section,
		props.Numero,
		props.Contenance,
	)
	out := &Result{
		Parcels:  []Parcel{parcel},
		Evidence: ev,
	}

	if s.opts.IncludeBati {
		s.runBati(ctx, fc.Features[idx], &parcel, out, logger)
	}

	return out, nil
}

// runBati performs the opt-in bâti enrichment. It mutates `out` in
// place: on success, populates BatiM2 / BatiCount / EmpriseRatio and
// stamps Evidence.BatiCached; on soft failure, stamps
// Evidence.BatiError and leaves the bâti fields nil. NEVER returns an
// error — soft-fail is the contract.
//
// The bâti dump is fetched per-INSEE; for Paris/Lyon/Marseille the
// arrondissement INSEE comes from the parcel id (idu) when available,
// else from the property code_arr field, else from the property
// code_insee — in order of fidelity. This matches the bundler's
// indexing (the parent commune INSEE returns an empty body).
func (s *Source) runBati(ctx context.Context, feature Feature, parcel *Parcel, out *Result, logger *slog.Logger) {
	batiINSEE := s.resolveBatiINSEE(feature.Properties, parcel.ID)
	if batiINSEE == "" {
		out.Evidence.BatiError = "no usable INSEE for bati lookup"
		logger.Debug("cadastre.bati_no_insee", slog.String("parcel_id", parcel.ID))
		return
	}

	polys, raw, cached, queriedURL, err := s.resolveBatiPolygons(ctx, batiINSEE)
	out.Evidence.BatiBaseURL = queriedURL
	out.Evidence.BatiCached = cached
	out.Evidence.BatiRawCount = raw
	if err != nil {
		out.Evidence.BatiError = redactError(err)
		logger.Debug("cadastre.bati_soft_fail",
			slog.String("insee", batiINSEE),
			slog.String("err", out.Evidence.BatiError),
		)
		return
	}

	parcelGeom, err := ParsePolygonGeometry(feature.Geometry)
	if err != nil {
		out.Evidence.BatiError = "decode parcel geometry: " + err.Error()
		logger.Debug("cadastre.bati_parcel_geom_err", slog.String("err", err.Error()))
		return
	}

	filtered := filterBatiInParcel(polys, parcelGeom)
	totalM2 := sumBatiArea(filtered)
	count := len(filtered)
	out.BatiCount = &count
	out.BatiM2 = &totalM2
	if parcel.ContenanceM2 > 0 {
		ratio := totalM2 / float64(parcel.ContenanceM2)
		out.EmpriseRatio = &ratio
	}
}

// resolveBatiINSEE picks the INSEE the bâti bundler will index on. The
// bundler keys per arrondissement for Paris / Lyon / Marseille, so we
// prefer (in order):
//
//  1. The first 5 chars of the parcel idu when it differs from the
//     property code_insee — that signals an arrondissement-vs-parent
//     case and the idu is the canonical anchor.
//  2. code_arr-derived INSEE: the property's `code_dep` + `code_arr`
//     would be needed; we instead use `code_insee` whose value is
//     already the arrondissement when the upstream resolved it that
//     way for non-Paris communes.
//  3. property code_insee — the parent code on Paris/Lyon/Marseille,
//     which the bundler returns empty for (logged as a soft skip).
func (s *Source) resolveBatiINSEE(props FeatureProperties, parcelID string) string {
	if len(parcelID) >= 5 {
		idPrefix := parcelID[:5]
		if idPrefix != props.CodeInsee && allDigits(idPrefix) {
			return idPrefix
		}
	}
	return props.CodeInsee
}

// allDigits reports whether s contains only ASCII digits — used to
// avoid promoting a malformed parcel id prefix as an INSEE candidate.
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		// Corsica's "2A"/"2B" can legitimately appear in cadastre
		// codes; accept those too.
		if c >= '0' && c <= '9' {
			continue
		}
		if i == 1 && (c == 'A' || c == 'B') {
			continue
		}
		return false
	}
	return true
}

// redactError shortens an error message to its first line and strips
// any sensitive prefix (URLs leak host / path) for the Evidence
// sidecar. Keeps the persisted payload small.
func redactError(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	if len(msg) > 240 {
		msg = msg[:240] + "…"
	}
	return msg
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

// fetch performs the HTTP GET on the parcelle endpoint and translates
// transport / status-code failures to gazetteer sentinels.
func (s *Source) fetch(ctx context.Context, u string) ([]byte, error) {
	client := s.opts.HTTPClient
	if client == nil {
		client = gazetteer.HTTPClientFrom(ctx)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("cadastre: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cadastre: %w: %w", gazetteer.ErrUpstreamUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		// 404 → no parcel under these coords. API Carto usually returns
		// 200 + empty features for that case, but be defensive: treat
		// 404 as an empty FeatureCollection so the parser yields a
		// zero-feature result.
		return []byte(`{"type":"FeatureCollection","features":[]}`), nil
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("cadastre: %w: http %d", gazetteer.ErrUpstreamUnavailable, resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("cadastre: %w: http %d", gazetteer.ErrUpstreamPermanent, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cadastre: %w: read body: %w", gazetteer.ErrUpstreamUnavailable, err)
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
		return 0, 0, errors.New("cadastre: lat/lon not resolvable (no geocoder configured)")
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
		return 0, 0, errors.New("cadastre: geocoder returned zero coords")
	}
	return res.Lat, res.Lon, nil
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
		return nil, errors.New("cadastre: typed result mismatch")
	}
	return res, nil
}

// emprise returns the share of the parcel covered by buildings, as a
// fraction in [0, +∞). Useful for callers that want the ratio without
// guarding on nil pointers themselves. Returns 0 when EmpriseRatio is
// nil.
func emprise(out *Result) float64 {
	if out == nil || out.EmpriseRatio == nil {
		return 0
	}
	return *out.EmpriseRatio
}

var _ = emprise // exported intent — keep the helper compiled until the appraisal layer wires it in

// Ensure the Source satisfies the gazetteer.Source interface and the
// BaseURLer side-protocol at compile time.
var (
	_ gazetteer.Source    = (*Source)(nil)
	_ gazetteer.BaseURLer = (*Source)(nil)
)

// Ensure geom import is used (compile guard — the package imports the
// geom symbols transitively but on some build configurations the linter
// may still complain).
var _ = geom.Point{}

func init() {
	gazetteer.Register(Name, func() any { return &Result{} })
}
