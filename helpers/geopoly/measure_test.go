package geopoly

import (
	"math"
	"testing"
)

// squareAtLat returns a closed 4-point ring forming an approximately
// `sizeM` × `sizeM` square centred on (lon0, lat0). Lon spacing is
// adjusted for the cosine factor so the planar area is honest at the
// chosen latitude. Used by the area / centroid goldens.
func squareAtLat(lon0, lat0, sizeM float64) Ring {
	halfDegLat := (sizeM / 2) / (math.Pi / 180.0 * EarthRadiusM)
	halfDegLon := halfDegLat / math.Cos(lat0*math.Pi/180.0)
	return Ring{
		{Lon: lon0 - halfDegLon, Lat: lat0 - halfDegLat},
		{Lon: lon0 + halfDegLon, Lat: lat0 - halfDegLat},
		{Lon: lon0 + halfDegLon, Lat: lat0 + halfDegLat},
		{Lon: lon0 - halfDegLon, Lat: lat0 + halfDegLat},
		{Lon: lon0 - halfDegLon, Lat: lat0 - halfDegLat}, // closing point
	}
}

// 100×100 m square at Paris latitude must yield 10 000 m² within 0.5 %.
// This is the load-bearing area calibration golden — never relax it
// without measuring why.
func TestRingAreaM2_HundredMeterSquareAt48_85(t *testing.T) {
	square := squareAtLat(2.35, 48.85, 100)
	got := square.AreaM2()
	want := 10000.0
	relErr := math.Abs(got-want) / want
	if relErr > 0.005 {
		t.Errorf("Ring.AreaM2 = %.2f, want ~%.2f (relErr %.4f > 0.005)", got, want, relErr)
	}
}

func TestRingAreaM2_TenMeterSquareAt45(t *testing.T) {
	square := squareAtLat(5.0, 45.0, 10)
	got := square.AreaM2()
	want := 100.0
	relErr := math.Abs(got-want) / want
	if relErr > 0.005 {
		t.Errorf("Ring.AreaM2 = %.4f, want ~%.2f (relErr %.4f)", got, want, relErr)
	}
}

func TestRingAreaM2_OpenAndClosedRingsAgree(t *testing.T) {
	closed := squareAtLat(2.35, 48.85, 100)
	open := append(Ring{}, closed[:4]...)
	a := closed.AreaM2()
	b := open.AreaM2()
	if math.Abs(a-b) > 1e-6 {
		t.Errorf("open vs closed ring area differ: %.6f vs %.6f", a, b)
	}
}

func TestRingAreaM2_DegenerateReturnsZero(t *testing.T) {
	tests := []struct {
		name string
		r    Ring
	}{
		{"empty", Ring{}},
		{"single", Ring{{Lon: 1, Lat: 1}}},
		{"two", Ring{{Lon: 1, Lat: 1}, {Lon: 2, Lat: 2}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.r.AreaM2(); got != 0 {
				t.Errorf("Ring.AreaM2(%s) = %v, want 0", tc.name, got)
			}
		})
	}
}

func TestPolygonAreaM2_SubtractsHoles(t *testing.T) {
	outer := squareAtLat(2.35, 48.85, 100)
	hole := squareAtLat(2.35, 48.85, 50)
	got := Polygon{outer, hole}.AreaM2()
	want := 10000.0 - 2500.0
	relErr := math.Abs(got-want) / want
	if relErr > 0.005 {
		t.Errorf("Polygon.AreaM2(holed) = %.2f, want %.2f (relErr %.4f)", got, want, relErr)
	}
}

func TestPolygonAreaM2_FloorsAtZero(t *testing.T) {
	small := squareAtLat(2.35, 48.85, 10)
	big := squareAtLat(2.35, 48.85, 100)
	// Degenerate input (hole bigger than the boundary) must not go
	// negative.
	if got := (Polygon{small, big}).AreaM2(); got != 0 {
		t.Errorf("Polygon.AreaM2(degenerate) = %v, want 0", got)
	}
}

func TestPolygonAreaM2_Empty(t *testing.T) {
	if got := (Polygon{}).AreaM2(); got != 0 {
		t.Errorf("Polygon.AreaM2(empty) = %v, want 0", got)
	}
}

func TestRingCentroid_SquareIsCentre(t *testing.T) {
	square := squareAtLat(2.35, 48.85, 100)
	c := square.Centroid()
	// 1e-6 degrees ~ 0.1 m — well below the cadastre's 1 mm precision.
	if math.Abs(c.Lon-2.35) > 1e-6 || math.Abs(c.Lat-48.85) > 1e-6 {
		t.Errorf("Ring.Centroid(square) = %+v, want ~(2.35, 48.85)", c)
	}
}

func TestRingCentroid_TriangleCorrectness(t *testing.T) {
	// Right triangle on the unit grid; expected centroid is the
	// arithmetic mean of the vertices ((1/3)(x0+x1+x2)).
	tri := Ring{
		{Lon: 0, Lat: 0},
		{Lon: 3, Lat: 0},
		{Lon: 0, Lat: 3},
		{Lon: 0, Lat: 0},
	}
	c := tri.Centroid()
	if math.Abs(c.Lon-1.0) > 1e-9 || math.Abs(c.Lat-1.0) > 1e-9 {
		t.Errorf("Ring.Centroid(triangle) = %+v, want (1,1)", c)
	}
}

func TestRingCentroid_DegenerateFallsBackToMean(t *testing.T) {
	// Collinear ring → Shoelace area == 0 → vertex-mean fallback.
	col := Ring{
		{Lon: 0, Lat: 0},
		{Lon: 1, Lat: 1},
		{Lon: 2, Lat: 2},
	}
	c := col.Centroid()
	if math.Abs(c.Lon-1.0) > 1e-9 || math.Abs(c.Lat-1.0) > 1e-9 {
		t.Errorf("Ring.Centroid(collinear) = %+v, want (1,1) via fallback", c)
	}
}

func TestRingCentroid_Empty(t *testing.T) {
	if c := (Ring{}).Centroid(); c != (Point{}) {
		t.Errorf("Ring.Centroid(empty) = %+v, want zero Point", c)
	}
}

func TestMultiPolygonAreaM2_SumsMembers(t *testing.T) {
	a := squareAtLat(0, 45, 100)
	b := squareAtLat(0.01, 45, 50)
	mp := MultiPolygon{Polygon{a}, Polygon{b}}
	got := mp.AreaM2()
	want := 10000.0 + 2500.0
	relErr := math.Abs(got-want) / want
	if relErr > 0.005 {
		t.Errorf("MultiPolygon.AreaM2 = %.2f, want %.2f (relErr %.4f)", got, want, relErr)
	}
}

func TestMultiPolygonCentroid_FirstPolygon(t *testing.T) {
	a := squareAtLat(2.35, 48.85, 100)
	b := squareAtLat(0.0, 0.0, 100)
	mp := MultiPolygon{Polygon{a}, Polygon{b}}
	c := mp.Centroid()
	if math.Abs(c.Lon-2.35) > 1e-6 || math.Abs(c.Lat-48.85) > 1e-6 {
		t.Errorf("MultiPolygon.Centroid = %+v, want (2.35, 48.85)", c)
	}
	if got := (MultiPolygon{}).Centroid(); got != (Point{}) {
		t.Errorf("MultiPolygon.Centroid(empty) = %+v, want zero Point", got)
	}
	if got := (Polygon{}).Centroid(); got != (Point{}) {
		t.Errorf("Polygon.Centroid(empty) = %+v, want zero Point", got)
	}
}
