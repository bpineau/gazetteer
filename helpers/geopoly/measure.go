package geopoly

import "math"

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

// Centroid returns the centroid of the first member polygon —
// sufficient as a representative point for "is this shape inside that
// area?" filters, where any point inside the shape works. Returns the
// zero Point for an empty MultiPolygon.
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

// AreaM2 returns the planar area of the polygon in square meters: the
// outer ring's area minus every subsequent ring's, per the GeoJSON
// convention that ring 0 is the boundary and rings 1+ are holes. The
// result is floored at 0 so degenerate ring sets (holes larger than
// the boundary, disjoint-ring "union" encodings) cannot go negative.
func (poly Polygon) AreaM2() float64 {
	if len(poly) == 0 {
		return 0
	}
	area := poly[0].AreaM2()
	for _, hole := range poly[1:] {
		area -= hole.AreaM2()
	}
	if area < 0 {
		return 0
	}
	return area
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
