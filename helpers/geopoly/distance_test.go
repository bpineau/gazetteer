package geopoly

import (
	"math"
	"testing"
)

// metricSquare is a ring around (lat0, lon0) that is halfM metres from centre
// to edge on BOTH axes. The longitude half-width is scaled by 1/cos(lat),
// without which the shape is a square in degrees and a rectangle in metres —
// and every distance assertion below would be reading whichever side happened
// to be nearer.
func metricSquare(lat0, lon0, halfM float64) Ring {
	dLat := halfM / metersPerDegreeLat
	dLon := halfM / (metersPerDegreeLat * math.Cos(lat0*math.Pi/180))
	return Ring{
		{Lon: lon0 - dLon, Lat: lat0 - dLat},
		{Lon: lon0 + dLon, Lat: lat0 - dLat},
		{Lon: lon0 + dLon, Lat: lat0 + dLat},
		{Lon: lon0 - dLon, Lat: lat0 + dLat},
	}
}

// TestBoundaryDistanceM_EdgeNotVertex is the whole point of the primitive: on
// a long edge, the nearest vertex can be kilometres away while the boundary
// itself is metres away. A 4 km edge with the query point opposite its middle
// puts the vertices 2 km off.
func TestBoundaryDistanceM_EdgeNotVertex(t *testing.T) {
	t.Parallel()
	const lat, lon = 48.0, 2.0
	// A horizontal edge 4 km long, centred on the query longitude, 100 m south.
	halfLonDeg := 2000.0 / (metersPerDegreeLat * math.Cos(lat*math.Pi/180))
	south := lat - 100.0/metersPerDegreeLat
	r := Ring{
		{Lon: lon - halfLonDeg, Lat: south},
		{Lon: lon + halfLonDeg, Lat: south},
		{Lon: lon + halfLonDeg, Lat: south - 0.01},
		{Lon: lon - halfLonDeg, Lat: south - 0.01},
	}
	got := r.BoundaryDistanceM(lat, lon)
	if math.Abs(got-100) > 1 {
		t.Errorf("BoundaryDistanceM = %.1f m, want ~100 (the edge, not the ~2000 m vertices)", got)
	}
}

// TestBoundaryDistanceM_Geometry pins the basic readings.
func TestBoundaryDistanceM_Geometry(t *testing.T) {
	t.Parallel()
	const lat, lon = 48.8566, 2.3522
	const half = 500.0 // a 1 km square, centre to edge
	dLat := half / metersPerDegreeLat
	mPerLon := metersPerDegreeLat * math.Cos(lat*math.Pi/180)
	dLon := half / mPerLon
	r := metricSquare(lat, lon, half)

	// The centre is half a side from every edge.
	if got := r.BoundaryDistanceM(lat, lon); math.Abs(got-500) > 2 {
		t.Errorf("centre: %.1f m, want ~500", got)
	}
	// A point on the boundary reads zero.
	if got := r.BoundaryDistanceM(lat+dLat, lon); got > 1 {
		t.Errorf("on the edge: %.1f m, want ~0", got)
	}
	// A point 200 m outside the northern edge.
	if got := r.BoundaryDistanceM(lat+dLat+200/metersPerDegreeLat, lon); math.Abs(got-200) > 2 {
		t.Errorf("200 m north: %.1f m, want ~200", got)
	}
	// Beyond a corner, the distance is to the corner itself: 300 m north and
	// 300 m east of the NE vertex is 300*sqrt(2) away.
	got := r.BoundaryDistanceM(lat+dLat+300/metersPerDegreeLat, lon+dLon+300/mPerLon)
	if want := 300 * math.Sqrt2; math.Abs(got-want) > 3 {
		t.Errorf("past the corner: %.1f m, want ~%.1f", got, want)
	}
}

// TestBoundaryDistanceM_HolesAndMembers checks that a hole's rim and every
// member of a MultiPolygon count as boundary.
func TestBoundaryDistanceM_HolesAndMembers(t *testing.T) {
	t.Parallel()
	const lat, lon = 48.0, 2.0
	outer := metricSquare(lat, lon, 2000)
	hole := metricSquare(lat, lon, 200)
	poly := Polygon{outer, hole}

	// At the centre of the hole the nearest boundary is the hole's rim.
	if got := poly.BoundaryDistanceM(lat, lon); math.Abs(got-200) > 2 {
		t.Errorf("centre of the hole: %.1f m, want ~200 (the rim, not the 2000 m outer edge)", got)
	}
	// An empty shape has no boundary.
	if got := (MultiPolygon{}).BoundaryDistanceM(lat, lon); !math.IsInf(got, 1) {
		t.Errorf("empty MultiPolygon: %v, want +Inf", got)
	}
	if got := (Ring{{Lon: lon, Lat: lat}}).BoundaryDistanceM(lat, lon); !math.IsInf(got, 1) {
		t.Errorf("degenerate ring: %v, want +Inf", got)
	}
	// The nearest member wins.
	far := metricSquare(lat+0.5, lon, 200)
	near := metricSquare(lat, lon+2000/(metersPerDegreeLat*math.Cos(lat*math.Pi/180)), 200)
	mp := MultiPolygon{Polygon{far}, Polygon{near}}
	if got := mp.BoundaryDistanceM(lat, lon); math.Abs(got-1800) > 5 {
		t.Errorf("nearest member: %.1f m, want ~1800", got)
	}
}

// TestBoundaryDistanceM_OverseasLatitudes checks the projection holds where
// the longitude scale is not the metropolitan one, including south of the
// equator.
func TestBoundaryDistanceM_OverseasLatitudes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name     string
		lat, lon float64
	}{
		{"Paris", 48.8566, 2.3522},
		{"Guadeloupe", 16.2650, -61.5510},
		{"Guyane", 4.9224, -52.3135},
		{"Reunion", -21.1151, 55.5364},
		{"Mayotte", -12.7809, 45.2278},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := metricSquare(c.lat, c.lon, 500)
			if got := r.BoundaryDistanceM(c.lat, c.lon); math.Abs(got-500) > 5 {
				t.Errorf("centre: %.1f m, want ~500", got)
			}
			mPerLon := metersPerDegreeLat * math.Cos(c.lat*math.Pi/180)
			east := c.lon + (500+300)/mPerLon
			if got := r.BoundaryDistanceM(c.lat, east); math.Abs(got-300) > 5 {
				t.Errorf("300 m east of the edge: %.1f m, want ~300", got)
			}
		})
	}
}
