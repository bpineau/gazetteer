package geopoly

import (
	"math"
	"sort"
)

// EarthRadiusM is the mean Earth radius used to convert angular
// distances (degrees) to meters. The IUGG mean is 6_371_000 m; that's
// the value WGS84 area calculations conventionally adopt and what most
// real-world geo libraries (turf.js, S2, …) ship with.
const EarthRadiusM = 6371000.0

// Centroid returns the area-weighted centroid of the ring, in lon/lat
// degrees. Implemented via the Shoelace centroid formula on the raw
// lon/lat coords — accurate enough as a "pick a point representative of
// the ring" tool for downstream point-in-polygon containment tests. For
// tiny / very thin rings (signed area ≈ 0) the function falls back to
// the arithmetic mean of the vertices, which is robust but coarse.
//
// A duplicated closing vertex (first point repeated last) is tolerated
// and dropped before the computation.
func (r Ring) Centroid() Point {
	n := len(r)
	if n == 0 {
		return Point{}
	}
	// Drop a duplicated closing vertex if present — the Shoelace
	// formula expects an open ring, repeating the first point would
	// double-count its contribution.
	ring := r
	if n >= 2 && ring[0] == ring[n-1] {
		ring = ring[:n-1]
		n = len(ring)
	}
	if n == 0 {
		return Point{}
	}
	if n == 1 {
		return ring[0]
	}
	var (
		signedArea2 float64
		cx          float64
		cy          float64
	)
	for i := 0; i < n; i++ {
		x0 := ring[i].Lon
		y0 := ring[i].Lat
		x1 := ring[(i+1)%n].Lon
		y1 := ring[(i+1)%n].Lat
		cross := x0*y1 - x1*y0
		signedArea2 += cross
		cx += (x0 + x1) * cross
		cy += (y0 + y1) * cross
	}
	if math.Abs(signedArea2) < 1e-18 {
		// Degenerate ring (collinear, single point repeated, etc.):
		// fall back to the vertex centroid.
		var sx, sy float64
		for _, v := range ring {
			sx += v.Lon
			sy += v.Lat
		}
		return Point{Lon: sx / float64(n), Lat: sy / float64(n)}
	}
	area6 := 3.0 * signedArea2
	return Point{Lon: cx / area6, Lat: cy / area6}
}

// Centroid returns the centroid of the polygon's outer ring (ring 0).
// Holes are ignored — the result is a representative point for the
// polygon's boundary, not the mass centroid of the holed shape. Returns
// the zero Point for an empty polygon.
func (poly Polygon) Centroid() Point {
	if len(poly) == 0 {
		return Point{}
	}
	return poly[0].Centroid()
}

// Centroid returns the centroid of the first member polygon. Returns the zero
// Point for an empty MultiPolygon.
//
// It is NOT a representative point for "is this shape inside that area?": it
// ignores every member but the first, and even for that one the shoelace
// centroid of a concave ring can fall outside the ring. Use
// Polygon.RepresentativePoint per member instead — sources/cadastre does, and
// the bug that taught it is written up there.
func (mp MultiPolygon) Centroid() Point {
	if len(mp) == 0 {
		return Point{}
	}
	return mp[0].Centroid()
}

// AreaM2 returns the planar area enclosed by the ring in square meters
// using a local equirectangular projection at the ring's centroid
// latitude. Accuracy is within ~0.5 % for shapes up to a few hectares
// at French latitudes — fine for a footprint readout.
//
// Formula:
//
//	x = lon * cos(lat0) * (π/180) * R
//	y = lat *            (π/180) * R
//	area = |Shoelace(x, y)|
//
// where lat0 is the centroid latitude (in radians). Choosing the local
// latitude as the projection origin keeps cos(lat) effectively
// constant over the ring, which is what makes the planar Shoelace
// honest in m². Rings with fewer than 3 distinct vertices yield 0.
func (r Ring) AreaM2() float64 {
	n := len(r)
	if n < 3 {
		return 0
	}
	c := r.Centroid()
	lat0Rad := c.Lat * math.Pi / 180.0
	kx := math.Cos(lat0Rad) * (math.Pi / 180.0) * EarthRadiusM
	ky := (math.Pi / 180.0) * EarthRadiusM
	ring := r
	if ring[0] == ring[n-1] {
		ring = ring[:n-1]
		n = len(ring)
	}
	if n < 3 {
		return 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		x0 := ring[i].Lon * kx
		y0 := ring[i].Lat * ky
		x1 := ring[(i+1)%n].Lon * kx
		y1 := ring[(i+1)%n].Lat * ky
		sum += x0*y1 - x1*y0
	}
	return math.Abs(sum) * 0.5
}

// AreaM2 returns the planar area of the polygon in square meters, under
// the SAME even-odd rule Covers applies: a ring nested inside an odd
// number of the polygon's other rings is subtracted, a ring nested
// inside an even number is added. The ordinary GeoJSON shape — ring 0
// the boundary, rings 1+ the holes — therefore measures boundary minus
// holes; two DISJOINT rings measure the sum of both; an island inside a
// hole is added back.
//
// It used to subtract every ring after the first unconditionally, which
// contradicted Covers on the disjoint case the Polygon godoc itself
// names: two detached 1 000 m² parcels packed into one Polygon (a real
// upstream encoding) both answered Covers true and measured 0 m²
// together. The result is still floored at 0 for genuinely degenerate
// input such as overlapping rings.
func (poly Polygon) AreaM2() float64 {
	var total float64
	for i, r := range poly {
		a := r.AreaM2()
		if a == 0 {
			continue
		}
		if poly.ringDepth(i)%2 == 0 {
			total += a
		} else {
			total -= a
		}
	}
	if total < 0 {
		return 0
	}
	return total
}

// ringDepth counts how many of the polygon's OTHER rings enclose ring i.
// Even means the ring adds area, odd means it takes some away — the
// per-ring form of the even-odd rule Covers applies per crossing.
//
// Containment is tested on a VERTEX of ring i, not on a point of its
// interior: the interior of an outer ring includes the inside of its own
// holes, so an interior point would report the boundary as nested in its
// own hole and flip its sign. A vertex sits on ring i and nowhere else,
// so for the non-crossing rings valid input is made of it answers
// exactly "is ring i inside ring j". A vertex landing exactly on another
// ring is undefined, like every boundary point in this package.
func (poly Polygon) ringDepth(i int) int {
	if len(poly[i]) == 0 {
		return 0
	}
	vertex := poly[i][0]
	depth := 0
	for j, other := range poly {
		if i == j {
			continue
		}
		if (Polygon{other}).Covers(vertex) {
			depth++
		}
	}
	return depth
}

// AreaM2 sums the planar area of every member polygon in square
// meters. For a typical building footprint with a single polygon this
// is just that polygon's area; the loop covers split footprints
// (disjoint pieces under one logical feature).
func (mp MultiPolygon) AreaM2() float64 {
	var sum float64
	for _, p := range mp {
		sum += p.AreaM2()
	}
	return sum
}

// RepresentativePoint returns a point GUARANTEED to lie inside the polygon
// (holes excluded), or ok=false for a polygon that encloses nothing.
//
// Centroid is not that point. The shoelace centroid of a concave ring can fall
// outside the ring entirely — an L-shaped building sits around its own
// centroid, not on it — and Polygon.Centroid ignores holes, so a ring-shaped
// polygon puts it in the courtyard. Both read as "this shape is not inside
// that area" to any Covers-based filter, which is how a building wholly inside
// its parcel came back with a zero footprint.
//
// Method: take the centroid when it is genuinely covered; otherwise sweep the
// horizontal line through it, collect the spans where that line is inside the
// polygon, and return the midpoint of the widest one. That is the standard
// "pole of inaccessibility, cheap edition" and it is exact for the simple
// polygons cadastral and administrative data are made of.
func (poly Polygon) RepresentativePoint() (Point, bool) {
	if len(poly) == 0 {
		return Point{}, false
	}
	if c := poly.Centroid(); poly.Covers(c) {
		return c, true
	}
	// Sweep at the outer ring's centroid latitude. Every crossing of any ring
	// toggles inside/outside, exactly as Covers does, so holes are handled by
	// the same even-odd rule rather than by a special case.
	y := poly[0].Centroid().Lat
	xs := ringCrossingsAt(poly, y)
	if len(xs) < 2 {
		return Point{}, false
	}
	sort.Float64s(xs)
	bestMid, bestWidth := 0.0, 0.0
	// Spans alternate inside/outside starting inside, so only even indices
	// open an interior span.
	for i := 0; i+1 < len(xs); i += 2 {
		if w := xs[i+1] - xs[i]; w > bestWidth {
			bestWidth, bestMid = w, (xs[i]+xs[i+1])/2
		}
	}
	if bestWidth <= 0 {
		return Point{}, false
	}
	return Point{Lon: bestMid, Lat: y}, true
}

// ringCrossingsAt returns the longitudes at which the horizontal line at
// latitude y crosses any of the polygon's ring edges. A half-open rule on the
// latitude span (lower end inclusive, upper end exclusive) counts a vertex
// exactly once, matching Covers.
func ringCrossingsAt(poly Polygon, y float64) []float64 {
	var xs []float64
	for _, r := range poly {
		if len(r) < 3 {
			continue
		}
		j := len(r) - 1
		for i := range r {
			a, b := r[j], r[i]
			j = i
			if (a.Lat > y) == (b.Lat > y) {
				continue
			}
			xs = append(xs, a.Lon+(y-a.Lat)/(b.Lat-a.Lat)*(b.Lon-a.Lon))
		}
	}
	return xs
}
