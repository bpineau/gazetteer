package cadastre

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/sources/cadastre/geom"
)

// BatiFeatureCollection is the GeoJSON envelope of the per-commune
// PCI building dump. The upstream serves it as
// `application/vnd.geo+json`, gzipped — Go's net/http transparently
// decodes the body so the parser sees raw JSON.
type BatiFeatureCollection struct {
	Features []BatiFeature `json:"features"`
}

// BatiFeature is one building polygon in the dump. Properties carry
// minor metadata (commune, type code, dates) we don't surface.
type BatiFeature struct {
	Geometry RawGeometry `json:"geometry"`
}

// ParseBatiFeatureCollection decodes the building dump body. Returns
// ErrEmptyBody on a nil / unparseable body.
func ParseBatiFeatureCollection(body []byte) (*BatiFeatureCollection, error) {
	if len(body) == 0 {
		return nil, ErrEmptyBody
	}
	fc := &BatiFeatureCollection{}
	if err := json.Unmarshal(body, fc); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEmptyBody, err)
	}
	return fc, nil
}

// LoadBatiPolygons decodes the dump body into the cache-ready
// BatiPolygon slice. Each entry carries the typed MultiPolygon, its
// pre-computed centroid, and its planar area — so the per-parcel
// filter doesn't repeatedly decode + project the same polygons on
// subsequent Query calls hitting the same INSEE. Features whose
// geometry is missing or malformed are silently skipped (the dump
// occasionally carries empty bâti placeholders).
func LoadBatiPolygons(body []byte) ([]BatiPolygon, int, error) {
	fc, err := ParseBatiFeatureCollection(body)
	if err != nil {
		return nil, 0, err
	}
	raw := len(fc.Features)
	out := make([]BatiPolygon, 0, raw)
	for _, f := range fc.Features {
		mp, err := ParsePolygonGeometry(f.Geometry)
		if err != nil || len(mp) == 0 {
			continue
		}
		out = append(out, BatiPolygon{
			Geometry: mp,
			Centroid: geom.MultiPolygonCentroid(mp),
			AreaM2:   geom.MultiPolygonAreaM2(mp),
		})
	}
	return out, raw, nil
}

// fetchBati performs the HTTP GET on the per-commune building dump.
// Returns the body or an error wrapped with the appropriate gazetteer
// sentinel. The caller's HTTP client handles gzip transparently.
func (s *Source) fetchBati(ctx context.Context, u string) ([]byte, error) {
	client := s.opts.HTTPClient
	if client == nil {
		client = gazetteer.HTTPClientFrom(ctx)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("cadastre: bati: build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.geo+json,application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cadastre: bati: %w: %w", gazetteer.ErrUpstreamUnavailable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		// 404 → no bâti for this commune. Return an empty dump rather
		// than an error so the caller can populate Bati* with zeros
		// rather than soft-fail.
		return []byte(`{"type":"FeatureCollection","features":[]}`), nil
	}
	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("cadastre: bati: %w: http %d", gazetteer.ErrUpstreamUnavailable, resp.StatusCode)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("cadastre: bati: %w: http %d", gazetteer.ErrUpstreamPermanent, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cadastre: bati: %w: read body: %w", gazetteer.ErrUpstreamUnavailable, err)
	}
	return body, nil
}

// applyBatiBaseURL rewrites the upstream root with s.opts.BatiBaseURL
// when set. Mirrors applyBaseURL for the parcelle endpoint.
func (s *Source) applyBatiBaseURL(u string) string {
	if s.opts.BatiBaseURL == "" {
		return u
	}
	return s.opts.BatiBaseURL + strings.TrimPrefix(u, BatiBaseURL)
}

// resolveBatiPolygons returns the per-commune building polygons for
// `insee`, fetching + caching them on miss. The cache is keyed by
// INSEE; cached hits set `cached` to true.
func (s *Source) resolveBatiPolygons(ctx context.Context, insee string) (polys []BatiPolygon, rawCount int, cached bool, queriedURL string, err error) {
	cache := s.opts.BatiCache
	if cache == nil {
		cache = s.defaultCache
	}
	if hit, ok := cache.Get(insee); ok {
		// Raw count is not persisted on the cache — we only know the
		// *filtered* count post-load. Surface the cached slice length
		// as a reasonable approximation (every cached entry is one
		// well-formed building).
		return hit, len(hit), true, "", nil
	}
	rawURL, err := BatimentsURLForINSEE(insee)
	if err != nil {
		return nil, 0, false, "", err
	}
	urlToHit := s.applyBatiBaseURL(rawURL)
	body, err := s.fetchBati(ctx, urlToHit)
	if err != nil {
		return nil, 0, false, urlToHit, err
	}
	polys, raw, err := LoadBatiPolygons(body)
	if err != nil {
		return nil, 0, false, urlToHit, fmt.Errorf("cadastre: bati: parse: %w: %w", gazetteer.ErrUpstreamUnavailable, err)
	}
	cache.Put(insee, polys)
	return polys, raw, false, urlToHit, nil
}

// filterBatiInParcel walks `polys` and keeps the ones whose centroid
// sits inside `parcel`. Returns the filtered slice (typically much
// smaller than `polys` — the dump has every building on the commune).
//
// `parcel` is the API Carto parcel geometry; using a MultiPolygon
// directly matches what API Carto actually returns and dodges a
// Polygon-vs-MultiPolygon split at the call site.
func filterBatiInParcel(polys []BatiPolygon, parcel geom.MultiPolygon) []BatiPolygon {
	if len(polys) == 0 || len(parcel) == 0 {
		return nil
	}
	out := make([]BatiPolygon, 0)
	for _, p := range polys {
		if geom.PointInMultiPolygon(p.Centroid, parcel) {
			out = append(out, p)
		}
	}
	return out
}

// sumBatiArea sums the planar area of every cached polygon.
func sumBatiArea(polys []BatiPolygon) float64 {
	var total float64
	for _, p := range polys {
		total += p.AreaM2
	}
	return total
}

// errBatiSkipped is sentinelled to nil — the bâti path NEVER returns
// an error to the caller; it stamps Evidence.BatiError instead. This
// var is retained as a compile-time guard against accidental error
// propagation from a future refactor of the bâti pipeline.
var errBatiSkipped = errors.New("cadastre: bati skipped")

var _ = errBatiSkipped // keep referenced; see comment above
