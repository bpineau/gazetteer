// Package geoindex factors out the "embedded gzipped polygon index +
// point-in-polygon resolve" scaffolding shared by the contour-backed sources
// (iris, qpv, encadrement). It sits one layer above the geometry kernel in
// [github.com/bpineau/gazetteer/helpers/geopoly]: geopoly owns the math
// (Covers / Bound / BBox); geoindex owns the plumbing that every source built
// on top of it had hand-rolled identically.
//
// Two concerns are shared, used at different times:
//
//   - Build time (a source's transform): the compact wire shape
//     [polygon][ring][vertex][lon,lat] and its codecs. [Compact] is that shape;
//     [DecodeGeoJSONGeometry] converts a GeoJSON Polygon/MultiPolygon to it
//     (rounding coordinates), and [Compact.MultiPolygon] / [FromMultiPolygon] /
//     [RoundCompact] convert and round. The wire shape is deliberately
//     unchanged from the per-source versions so existing committed *.json.gz
//     artifacts still decode byte-for-byte.
//
//   - Query time (a source's loader): [Index] is a generic, payload-parameterised
//     bag of [Feature]s with a bbox-prefiltered first-cover [Index.Resolve]
//     scan (plus [Index.ResolveWhere] for a candidate predicate and
//     [Index.Nearest] for the vertex-distance fallback). The source controls
//     feature order before building the Index, so first-cover ties resolve
//     deterministically exactly as before.
//
// The payload is opaque (a type parameter), because the three sources carry
// different per-feature data: iris {code,nom,typ}, qpv {code,label}, encadrement
// {ept,zone,insee,commune}. Only the geometry plumbing is shared.
package geoindex

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/bpineau/gazetteer/helpers/geodist"
	"github.com/bpineau/gazetteer/helpers/geopoly"
)

// Compact is the compact wire shape for a feature's geometry:
// [polygon][ring][vertex][lon,lat]. It marshals to / from JSON as the bare
// nested float array (no wrapper), so a source embeds it under whatever JSON
// tag it likes (conventionally "g") and the committed artifacts stay
// byte-compatible.
type Compact [][][][2]float64

// MultiPolygon converts the compact shape into a geopoly.MultiPolygon, mapping
// each [lon, lat] vertex to geopoly.Point{Lon, Lat}.
func (c Compact) MultiPolygon() geopoly.MultiPolygon {
	mp := make(geopoly.MultiPolygon, 0, len(c))
	for _, poly := range c {
		gp := make(geopoly.Polygon, 0, len(poly))
		for _, ring := range poly {
			gr := make(geopoly.Ring, 0, len(ring))
			for _, v := range ring {
				gr = append(gr, geopoly.Point{Lon: v[0], Lat: v[1]})
			}
			gp = append(gp, gr)
		}
		mp = append(mp, gp)
	}
	return mp
}

// FromMultiPolygon converts a geopoly.MultiPolygon back into the compact wire
// shape. It is the inverse of [Compact.MultiPolygon].
func FromMultiPolygon(mp geopoly.MultiPolygon) Compact {
	c := make(Compact, 0, len(mp))
	for _, poly := range mp {
		cp := make([][][2]float64, 0, len(poly))
		for _, ring := range poly {
			cr := make([][2]float64, 0, len(ring))
			for _, pt := range ring {
				cr = append(cr, [2]float64{pt.Lon, pt.Lat})
			}
			cp = append(cp, cr)
		}
		c = append(c, cp)
	}
	return c
}

// RoundCompact returns a copy of c with every vertex rounded to the given
// number of decimal places. Rounding keeps the embedded artifact compact;
// callers pick decimals to match their boundary fidelity (e.g. 5 ≈ 1 m).
func RoundCompact(c Compact, decimals int) Compact {
	out := make(Compact, 0, len(c))
	for _, poly := range c {
		op := make([][][2]float64, 0, len(poly))
		for _, ring := range poly {
			or := make([][2]float64, 0, len(ring))
			for _, v := range ring {
				or = append(or, [2]float64{roundTo(v[0], decimals), roundTo(v[1], decimals)})
			}
			op = append(op, or)
		}
		out = append(out, op)
	}
	return out
}

// DecodeGeoJSONGeometry normalises a GeoJSON "Polygon" or "MultiPolygon"
// geometry (its type tag and raw coordinates) into the compact
// [polygon][ring][vertex][lon,lat] shape, rounding coordinates to decimals.
//
// Vertices may carry a third (altitude) ordinate upstream; only lon/lat are
// kept. Coordinate entries with fewer than two ordinates are dropped
// defensively. An unsupported geometry type is an error.
func DecodeGeoJSONGeometry(typ string, coords json.RawMessage, decimals int) (Compact, error) {
	switch typ {
	case "Polygon":
		var p [][][]float64
		if err := json.Unmarshal(coords, &p); err != nil {
			return nil, fmt.Errorf("polygon coords: %w", err)
		}
		return Compact{roundRings(p, decimals)}, nil
	case "MultiPolygon":
		var mp [][][][]float64
		if err := json.Unmarshal(coords, &mp); err != nil {
			return nil, fmt.Errorf("multipolygon coords: %w", err)
		}
		out := make(Compact, 0, len(mp))
		for _, p := range mp {
			out = append(out, roundRings(p, decimals))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported geometry type %q", typ)
	}
}

// roundRings converts and rounds one polygon's rings, dropping malformed
// (<2-ordinate) vertices.
func roundRings(rings [][][]float64, decimals int) [][][2]float64 {
	out := make([][][2]float64, 0, len(rings))
	for _, ring := range rings {
		rr := make([][2]float64, 0, len(ring))
		for _, v := range ring {
			if len(v) < 2 {
				continue
			}
			rr = append(rr, [2]float64{roundTo(v[0], decimals), roundTo(v[1], decimals)})
		}
		out = append(out, rr)
	}
	return out
}

func roundTo(f float64, decimals int) float64 {
	p := math.Pow(10, float64(decimals))
	return math.Round(f*p) / p
}

// Feature is one resolvable area: an opaque payload, its geometry, and a
// precomputed bounding box used to reject non-candidates before the O(n)
// point-in-polygon test. Build one with [NewFeature].
type Feature[T any] struct {
	// Payload is the source-specific per-feature data (identity, labels, …).
	Payload T

	mp   geopoly.MultiPolygon
	bbox geopoly.BBox
}

// NewFeature builds a Feature, precomputing its bounding box via
// geopoly.MultiPolygon.Bound.
func NewFeature[T any](payload T, mp geopoly.MultiPolygon) Feature[T] {
	return Feature[T]{Payload: payload, mp: mp, bbox: mp.Bound()}
}

// Index is a payload-parameterised set of [Feature]s supporting a
// bbox-prefiltered point-in-polygon scan. Feature order is caller-controlled
// (the source builds it in code-sorted order) so first-cover ties resolve
// deterministically.
type Index[T any] struct {
	feats []Feature[T]
}

// New builds an Index from the given features, preserving their order. The
// caller owns determinism: sort features (e.g. by code) before calling New if
// boundary-tie order matters.
func New[T any](feats []Feature[T]) *Index[T] {
	return &Index[T]{feats: feats}
}

// Len reports the number of features in the index. A nil index has length 0.
func (idx *Index[T]) Len() int {
	if idx == nil {
		return 0
	}
	return len(idx.feats)
}

// Resolve returns the payload of the first feature whose geometry covers
// (lat, lon). ok is false when the point lies outside every feature. The bbox
// pre-filter keeps the scan cheap. On a shared boundary the first covering
// feature in index order wins deterministically.
func (idx *Index[T]) Resolve(lat, lon float64) (T, bool) {
	return idx.ResolveWhere(lat, lon, nil)
}

// ResolveWhere is Resolve restricted to features for which keep(payload) is
// true. A nil keep accepts every feature (then it is exactly Resolve). It lets
// a source scope the scan to candidates (e.g. only the listing's own commune)
// without leaving the shared scan.
func (idx *Index[T]) ResolveWhere(lat, lon float64, keep func(T) bool) (T, bool) {
	var zero T
	if idx == nil {
		return zero, false
	}
	p := geopoly.Point{Lon: lon, Lat: lat}
	for i := range idx.feats {
		f := &idx.feats[i]
		if keep != nil && !keep(f.Payload) {
			continue
		}
		if f.bbox.Contains(p) && f.mp.Covers(p) {
			return f.Payload, true
		}
	}
	return zero, false
}

// metersPerDegreeLat is the mean length of one degree of latitude. Used
// for the conservative degree-space pre-reject in Nearest (with a slack
// factor absorbing the small real-world variation, 110.57–111.69 km).
const metersPerDegreeLat = 111_320.0

// Nearest returns the payload of the feature with the smallest vertex distance
// to (lat, lon), that distance in metres, and ok=true — considering only
// features with a vertex within maxMeters. ok is false when none qualifies.
//
// Distance is the minimum great-circle distance from the point to any boundary
// *vertex* (not the polygon edge), via [geodist.MetersBetween] — a cheap
// "is there a QPV nearby?" hint, not an exact distance-to-boundary.
//
// Features whose bounding box, expanded by maxMeters, does not contain the
// point are rejected without touching their vertices — in the common
// "point is far from every feature" case this skips >99 % of the vertex
// distance computations.
func (idx *Index[T]) Nearest(lat, lon, maxMeters float64) (T, float64, bool) {
	var best T
	if idx == nil {
		return best, 0, false
	}
	// Degree-space expansion of maxMeters, with 10 % slack so the
	// approximation can only over-accept (the exact metric test below
	// still decides), never wrongly reject.
	dLat := maxMeters / metersPerDegreeLat * 1.1
	dLon := dLat / math.Max(math.Cos(lat*math.Pi/180), 0.01)
	bestDist := maxMeters
	found := false
	for i := range idx.feats {
		f := &idx.feats[i]
		b := f.bbox
		if lat < b.MinLat-dLat || lat > b.MaxLat+dLat ||
			lon < b.MinLon-dLon || lon > b.MaxLon+dLon {
			continue
		}
		for _, polygon := range f.mp {
			for _, ring := range polygon {
				for _, v := range ring {
					d := geodist.MetersBetween(lat, lon, v.Lat, v.Lon)
					if d < bestDist {
						bestDist = d
						best = f.Payload
						found = true
					}
				}
			}
		}
	}
	if !found {
		var zero T
		return zero, 0, false
	}
	return best, bestDist, true
}
