package cadastre

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/bpineau/gazetteer/helpers/geopoly"
)

// ErrEmptyBody is returned by ParseFeatureCollection when the input is
// empty or not parseable as JSON. The Source wraps it as
// gazetteer.ErrUpstreamUnavailable.
var ErrEmptyBody = errors.New("cadastre: empty / unparseable body")

// FeatureCollection is the GeoJSON envelope the API Carto cadastre
// endpoint returns. Only the fields the Source actually consumes are
// modelled — `type`, `id`, CRS, `geometry_name` and timestamps are
// ignored.
type FeatureCollection struct {
	Features []Feature `json:"features"`
}

// Feature is one parcel inside a FeatureCollection. The upstream
// `geometry.type` is "MultiPolygon" in practice (verified live
// 2026-05-28) — the Polygon branch in ParsePolygonGeometry is kept as
// a safety net but is not exercised on the happy path.
type Feature struct {
	Geometry   RawGeometry       `json:"geometry"`
	Properties FeatureProperties `json:"properties"`
}

// FeatureProperties carries the cadastre payload on each parcel.
//
// Naming notes:
//   - `code_insee` is the COMMUNE INSEE (for Paris/Lyon/Marseille this
//     is the parent code, e.g. 75056 — NOT the arrondissement).
//   - `com_abs` is the PREFIXE component of the Etalab parcel id. The
//     API does not expose a `prefixe` field directly; com_abs is what
//     fits the id structure.
//   - `section` is already 2-char zero-padded by the upstream.
//   - `numero` is 4-char zero-padded.
//   - `idu`, when present, embeds the ARRONDISSEMENT INSEE (75104) for
//     Paris/Lyon/Marseille parcels — that's why we prefer it over a
//     recomposed id.
type FeatureProperties struct {
	CodeInsee  string `json:"code_insee"`
	ComAbs     string `json:"com_abs"`
	Section    string `json:"section"`
	Numero     string `json:"numero"`
	Contenance int    `json:"contenance"`
	IDU        string `json:"idu"`
	CodeArr    string `json:"code_arr"`
}

// RawGeometry is the GeoJSON geometry blob attached to each feature.
// We keep coordinates as `json.RawMessage` to disambiguate Polygon
// vs MultiPolygon at decode time without pulling a heavy decoder.
type RawGeometry struct {
	Type        string          `json:"type"`
	Coordinates json.RawMessage `json:"coordinates"`
}

// ParseFeatureCollection decodes the API Carto JSON body into a
// FeatureCollection. Returns ErrEmptyBody on a nil / unparseable body.
// An empty `features` array is returned without error so callers can
// distinguish "no results" from a parser failure.
func ParseFeatureCollection(body []byte) (*FeatureCollection, error) {
	if len(body) == 0 {
		return nil, ErrEmptyBody
	}
	fc := &FeatureCollection{}
	if err := json.Unmarshal(body, fc); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEmptyBody, err)
	}
	return fc, nil
}

// ParsePolygonGeometry decodes a Feature's geometry into a typed
// geopoly.MultiPolygon. The API Carto endpoint always returns
// "MultiPolygon" today; the "Polygon" arm is a safety net for
// upstream-schema drift. Other geometry types return an error so
// callers can skip the feature without misinterpreting it as a parcel
// polygon.
//
// The decode is deliberately local rather than reusing
// geoindex.DecodeGeoJSONGeometry: that helper rounds coordinates (for
// committed embedded artifacts) and retains inner rings, while this
// runtime path must keep the upstream's full 8-decimal precision and
// keeps only the OUTER ring of each polygon — cadastre parcels never
// have holes (per the Etalab documentation), and dropping inner rings
// also keeps the bâti footprint areas computed downstream stable for
// courtyard buildings.
func ParsePolygonGeometry(g RawGeometry) (geopoly.MultiPolygon, error) {
	switch g.Type {
	case "MultiPolygon":
		// GeoJSON MultiPolygon coordinates: [[[[lon,lat], ...]], ...]
		var raw [][][][2]float64
		if err := json.Unmarshal(g.Coordinates, &raw); err != nil {
			return nil, fmt.Errorf("cadastre: decode MultiPolygon coords: %w", err)
		}
		mp := make(geopoly.MultiPolygon, 0, len(raw))
		for _, poly := range raw {
			if len(poly) == 0 {
				continue
			}
			mp = append(mp, geopoly.Polygon{outerRing(poly[0])})
		}
		return mp, nil
	case "Polygon":
		var raw [][][2]float64
		if err := json.Unmarshal(g.Coordinates, &raw); err != nil {
			return nil, fmt.Errorf("cadastre: decode Polygon coords: %w", err)
		}
		if len(raw) == 0 {
			return nil, nil
		}
		return geopoly.MultiPolygon{geopoly.Polygon{outerRing(raw[0])}}, nil
	default:
		return nil, fmt.Errorf("cadastre: unsupported geometry type %q", g.Type)
	}
}

// outerRing converts one decoded GeoJSON ring into a geopoly.Ring.
func outerRing(pts [][2]float64) geopoly.Ring {
	ring := make(geopoly.Ring, 0, len(pts))
	for _, pt := range pts {
		ring = append(ring, geopoly.Point{Lon: pt[0], Lat: pt[1]})
	}
	return ring
}

// PickFeature returns the first feature whose polygon contains the
// query point, falling back to the first feature when none claim the
// point (typical edge case: the listing's lat/lon landed on a parcel
// boundary). Returns (-1, false) on an empty list.
//
// The fallback is deliberate — API Carto already filtered to parcels
// near the query point, so the "first feature" is by construction a
// near-miss rather than a random pick.
func PickFeature(features []Feature, lon, lat float64) (int, bool) {
	if len(features) == 0 {
		return -1, false
	}
	p := geopoly.Point{Lon: lon, Lat: lat}
	for i, f := range features {
		mp, err := ParsePolygonGeometry(f.Geometry)
		if err != nil {
			continue
		}
		if mp.Covers(p) {
			return i, true
		}
	}
	// No containment hit — first feature wins by convention.
	return 0, true
}
