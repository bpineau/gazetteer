// Package geom holds the minimal planar / spherical geometry helpers
// the cadastre Source needs to (a) decide whether a parcel polygon
// contains the listing's lat/lon and (b) compute the planar area of
// each bâtiment footprint in m² with single-percent accuracy at French
// latitudes.
//
// All inputs are GeoJSON-shaped lon/lat pairs (degrees). We do NOT
// pull a heavy geo dependency — the algorithms below are textbook ray
// casting + Shoelace, with a small equirectangular projection applied
// per polygon to keep the planar area honest. Validated against a
// known 100×100 m square at latitude 48.85 in geom_test.go.
package geom

import "math"

// EarthRadiusM is the mean Earth radius used to convert angular
// distances (degrees) to meters. The IUGG mean is 6_371_000 m; that's
// the value WGS84 area calculations conventionally adopt and what most
// real-world geo libraries (turf.js, S2, …) ship with.
const EarthRadiusM = 6371000.0

// Point is a lon/lat pair in degrees. The X-first / Y-second order
// matches GeoJSON Position so callers can feed raw decoded coords
// without reordering.
type Point struct {
	Lon float64
	Lat float64
}

// Polygon is the outer ring of a GeoJSON Polygon (in lon/lat degrees).
// Cadastre parcels never have holes — confirmed by the Etalab
// documentation — so we ignore inner rings throughout the package.
//
// The ring is expected to be closed (first point == last point) but
// the algorithms work either way; the closing segment is implicit.
type Polygon []Point

// MultiPolygon groups several Polygons under one GeoJSON feature. The
// cadastre parcelle endpoint always returns MultiPolygon geometries
// (even single-polygon parcels are wrapped), so this is the canonical
// containment unit downstream.
type MultiPolygon []Polygon

// PointInRing reports whether p sits inside ring using a standard
// ray-casting test. A point ON an edge is conventionally reported as
// inside on the right / bottom edges and outside on the left / top;
// the asymmetry is acceptable because cadastre coordinates carry 8
// decimals (~1 mm) — exact-edge ties never arise on real inputs.
func PointInRing(p Point, ring []Point) bool {
	n := len(ring)
	if n < 3 {
		return false
	}
	inside := false
	j := n - 1
	for i := 0; i < n; i++ {
		yi := ring[i].Lat
		yj := ring[j].Lat
		xi := ring[i].Lon
		xj := ring[j].Lon
		if (yi > p.Lat) != (yj > p.Lat) {
			// Linear interpolation of x on the segment at y == p.Lat.
			xCross := (xj-xi)*(p.Lat-yi)/(yj-yi) + xi
			if p.Lon < xCross {
				inside = !inside
			}
		}
		j = i
	}
	return inside
}

// PointInPolygon reports whether p is inside the polygon's outer ring.
// We deliberately ignore inner rings (cadastre parcels have none).
func PointInPolygon(p Point, polygon Polygon) bool {
	return PointInRing(p, polygon)
}

// PointInMultiPolygon reports whether p is inside ANY polygon of mp.
// Cadastre features are usually MultiPolygons with a single member but
// occasionally split parcels (riverside lots, disjoint co-properties)
// surface as multi-member geometries.
func PointInMultiPolygon(p Point, mp MultiPolygon) bool {
	for _, poly := range mp {
		if PointInPolygon(p, poly) {
			return true
		}
	}
	return false
}

// Centroid returns the area-weighted centroid of a polygon's outer
// ring, in lon/lat degrees. Implemented via the Shoelace centroid
// formula on the raw lon/lat coords — accurate enough as a "pick a
// point representative of the polygon" tool for downstream point-in-
// polygon containment tests. For tiny / very thin polygons (signed
// area ≈ 0) the function falls back to the arithmetic mean of the
// vertices, which is robust but coarse.
func Centroid(polygon Polygon) Point {
	n := len(polygon)
	if n == 0 {
		return Point{}
	}
	// Drop a duplicated closing vertex if present — the Shoelace
	// formula expects an open ring, repeating the first point would
	// double-count its contribution.
	ring := polygon
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
		// Degenerate polygon (collinear, single point repeated, etc.):
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

// MultiPolygonCentroid returns the centroid of the first polygon of mp
// — sufficient for the "centroid in parcel?" filter, where any point
// representative of the building works as long as it sits inside the
// building.
func MultiPolygonCentroid(mp MultiPolygon) Point {
	if len(mp) == 0 {
		return Point{}
	}
	return Centroid(mp[0])
}

// PolygonAreaM2 returns the planar area of the polygon in square
// meters using a local equirectangular projection at the polygon's
// centroid latitude. Accuracy is within ~0.5 % for parcels up to a few
// hectares at French latitudes — fine for an UI footprint readout.
//
// Formula:
//
//	x = lon * cos(lat0) * (π/180) * R
//	y = lat *            (π/180) * R
//	area = |Shoelace(x, y)|
//
// where lat0 is the centroid latitude (in radians). Choosing the local
// latitude as the projection origin keeps cos(lat) effectively
// constant over the polygon, which is what makes the planar Shoelace
// honest in m².
func PolygonAreaM2(polygon Polygon) float64 {
	n := len(polygon)
	if n < 3 {
		return 0
	}
	c := Centroid(polygon)
	lat0Rad := c.Lat * math.Pi / 180.0
	kx := math.Cos(lat0Rad) * (math.Pi / 180.0) * EarthRadiusM
	ky := (math.Pi / 180.0) * EarthRadiusM
	ring := polygon
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

// MultiPolygonAreaM2 sums the planar area of every polygon in mp.
// For a typical building footprint with a single outer ring this is
// just PolygonAreaM2 of the only ring; the loop covers split footprints
// (Tour Eiffel-style disjoint pieces).
func MultiPolygonAreaM2(mp MultiPolygon) float64 {
	var sum float64
	for _, p := range mp {
		sum += PolygonAreaM2(p)
	}
	return sum
}
