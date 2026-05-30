package geopoly

import "math"

// Point is a (lon, lat) coordinate in decimal degrees. Lon is the X axis and
// Lat the Y axis — the GeoJSON axis order.
type Point struct {
	Lon, Lat float64
}

// Ring is a sequence of points forming a closed loop. The ring is treated as
// implicitly closed: the first and last vertex need not be equal. Rings with
// fewer than 3 points are ignored by the coverage test.
type Ring []Point

// Polygon is a ring set evaluated under the even-odd rule (see Covers).
// Conventionally ring 0 is the outer boundary and any further rings are holes
// punched out of it — but the rule is purely even-odd, so two *disjoint* rings
// union (the shape some upstreams use to pack several detached parcels into one
// "Polygon"), while a *nested* inner ring subtracts. Overlapping rings are
// degenerate input and produce the symmetric difference.
type Polygon []Ring

// MultiPolygon is a set of polygons making up one logical area (e.g. a commune
// with detached parcels). A point is covered when it falls in any member
// polygon. The zero value (nil) covers nothing.
type MultiPolygon []Polygon

// Covers reports whether p lies inside the polygon, holes excluded. It applies
// the even-odd rule across every ring at once: a crossing of any ring (outer or
// hole) toggles membership, so a point inside a hole nets to "outside"
// automatically. Boundary points are undefined (see package doc).
func (poly Polygon) Covers(p Point) bool {
	inside := false
	for _, r := range poly {
		if len(r) < 3 {
			continue
		}
		j := len(r) - 1
		for i := range r {
			a, b := r[i], r[j]
			if (a.Lat > p.Lat) != (b.Lat > p.Lat) &&
				p.Lon < (b.Lon-a.Lon)*(p.Lat-a.Lat)/(b.Lat-a.Lat)+a.Lon {
				inside = !inside
			}
			j = i
		}
	}
	return inside
}

// Covers reports whether p lies inside any member polygon.
func (mp MultiPolygon) Covers(p Point) bool {
	for _, poly := range mp {
		if poly.Covers(p) {
			return true
		}
	}
	return false
}

// BBox is an axis-aligned bounding box in (lon, lat) degrees. The zero-area
// "empty" box returned by Bound for an empty geometry has Min > Max on both
// axes, so Contains reports false for every point — letting callers use it as a
// cheap pre-reject without a separate emptiness flag.
type BBox struct {
	MinLon, MinLat, MaxLon, MaxLat float64
}

// Contains reports whether p falls within the box (inclusive bounds).
func (b BBox) Contains(p Point) bool {
	return p.Lon >= b.MinLon && p.Lon <= b.MaxLon &&
		p.Lat >= b.MinLat && p.Lat <= b.MaxLat
}

// Bound returns the bounding box enclosing every vertex of the MultiPolygon.
//
// It spans all rings, not just ring 0: real-world data sometimes encodes
// several disjoint regions as one Polygon with multiple top-level rings (the
// even-odd rule unions them — see Covers), and such rings can extend beyond
// ring 0. A box that only spanned ring 0 would let a Covers-prefilter wrongly
// reject points that lie in another ring. True holes lie inside their outer
// ring and so do not widen the box.
//
// An empty MultiPolygon yields the inverted-infinity box whose Contains is
// always false. Callers store the box alongside the geometry and test it before
// the O(n) Covers scan when probing many areas with one point.
func (mp MultiPolygon) Bound() BBox {
	b := BBox{
		MinLon: math.Inf(1), MinLat: math.Inf(1),
		MaxLon: math.Inf(-1), MaxLat: math.Inf(-1),
	}
	for _, poly := range mp {
		for _, ring := range poly {
			for _, pt := range ring {
				b.MinLon = math.Min(b.MinLon, pt.Lon)
				b.MinLat = math.Min(b.MinLat, pt.Lat)
				b.MaxLon = math.Max(b.MaxLon, pt.Lon)
				b.MaxLat = math.Max(b.MaxLat, pt.Lat)
			}
		}
	}
	return b
}
