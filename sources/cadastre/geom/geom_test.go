package geom

import (
	"math"
	"testing"
)

// squareAtLat returns a closed 4-point ring forming an approximately
// `sizeM` × `sizeM` square centred on (lon0, lat0). Lon spacing is
// adjusted for the cosine factor so the planar area is honest at the
// chosen latitude. Used by the area / containment goldens.
func squareAtLat(lon0, lat0, sizeM float64) Polygon {
	halfDegLat := (sizeM / 2) / (math.Pi / 180.0 * EarthRadiusM)
	halfDegLon := halfDegLat / math.Cos(lat0*math.Pi/180.0)
	return Polygon{
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
func TestPolygonAreaM2_HundredMeterSquareAt48_85(t *testing.T) {
	square := squareAtLat(2.35, 48.85, 100)
	got := PolygonAreaM2(square)
	want := 10000.0
	relErr := math.Abs(got-want) / want
	if relErr > 0.005 {
		t.Errorf("PolygonAreaM2 = %.2f, want ~%.2f (relErr %.4f > 0.005)", got, want, relErr)
	}
}

func TestPolygonAreaM2_TenMeterSquareAt45(t *testing.T) {
	square := squareAtLat(5.0, 45.0, 10)
	got := PolygonAreaM2(square)
	want := 100.0
	relErr := math.Abs(got-want) / want
	if relErr > 0.005 {
		t.Errorf("PolygonAreaM2 = %.4f, want ~%.2f (relErr %.4f)", got, want, relErr)
	}
}

func TestPolygonAreaM2_OpenAndClosedRingsAgree(t *testing.T) {
	closed := squareAtLat(2.35, 48.85, 100)
	open := append(Polygon{}, closed[:4]...)
	a := PolygonAreaM2(closed)
	b := PolygonAreaM2(open)
	if math.Abs(a-b) > 1e-6 {
		t.Errorf("open vs closed ring area differ: %.6f vs %.6f", a, b)
	}
}

func TestPolygonAreaM2_DegenerateRetursZero(t *testing.T) {
	tests := []struct {
		name string
		p    Polygon
	}{
		{"empty", Polygon{}},
		{"single", Polygon{{Lon: 1, Lat: 1}}},
		{"two", Polygon{{Lon: 1, Lat: 1}, {Lon: 2, Lat: 2}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := PolygonAreaM2(tc.p); got != 0 {
				t.Errorf("PolygonAreaM2(%s) = %v, want 0", tc.name, got)
			}
		})
	}
}

func TestPointInRing_Inside(t *testing.T) {
	square := squareAtLat(2.35, 48.85, 100)
	if !PointInRing(Point{Lon: 2.35, Lat: 48.85}, square) {
		t.Error("centre of the square reported outside")
	}
}

func TestPointInRing_Outside(t *testing.T) {
	square := squareAtLat(2.35, 48.85, 100)
	// 1 km away — way outside the 100 m square.
	if PointInRing(Point{Lon: 2.36, Lat: 48.85}, square) {
		t.Error("far point reported inside")
	}
}

func TestPointInPolygon_ConcaveShape(t *testing.T) {
	// L-shape (concave): big square with a top-right notch cut out.
	// We use plain unit-degree coords; only the ray cast topology matters.
	poly := Polygon{
		{Lon: 0, Lat: 0},
		{Lon: 2, Lat: 0},
		{Lon: 2, Lat: 1},
		{Lon: 1, Lat: 1},
		{Lon: 1, Lat: 2},
		{Lon: 0, Lat: 2},
		{Lon: 0, Lat: 0},
	}
	// (0.5, 0.5) — inside the bottom part of the L.
	if !PointInPolygon(Point{Lon: 0.5, Lat: 0.5}, poly) {
		t.Error("(0.5, 0.5) reported outside the L")
	}
	// (1.5, 1.5) — inside the cut-out notch, outside the L.
	if PointInPolygon(Point{Lon: 1.5, Lat: 1.5}, poly) {
		t.Error("(1.5, 1.5) inside the notch reported inside")
	}
	// (1.5, 0.5) — still inside the L (lower band).
	if !PointInPolygon(Point{Lon: 1.5, Lat: 0.5}, poly) {
		t.Error("(1.5, 0.5) reported outside")
	}
}

func TestPointInMultiPolygon_AnyMember(t *testing.T) {
	a := squareAtLat(0, 0, 100)
	b := squareAtLat(0.01, 0, 100) // ~1.1 km east — disjoint
	mp := MultiPolygon{a, b}
	if !PointInMultiPolygon(Point{Lon: 0, Lat: 0}, mp) {
		t.Error("centre of A reported outside the union")
	}
	if !PointInMultiPolygon(Point{Lon: 0.01, Lat: 0}, mp) {
		t.Error("centre of B reported outside the union")
	}
	if PointInMultiPolygon(Point{Lon: 1.0, Lat: 1.0}, mp) {
		t.Error("far point reported inside the union")
	}
}

func TestCentroid_SquareIsCentre(t *testing.T) {
	square := squareAtLat(2.35, 48.85, 100)
	c := Centroid(square)
	// 1e-6 degrees ~ 0.1 m — well below the cadastre's 1 mm precision.
	if math.Abs(c.Lon-2.35) > 1e-6 || math.Abs(c.Lat-48.85) > 1e-6 {
		t.Errorf("Centroid(square) = %+v, want ~(2.35, 48.85)", c)
	}
}

func TestCentroid_TriangleCorrectness(t *testing.T) {
	// Right triangle on the unit grid; expected centroid is the
	// arithmetic mean of the vertices ((1/3)(x0+x1+x2)).
	tri := Polygon{
		{Lon: 0, Lat: 0},
		{Lon: 3, Lat: 0},
		{Lon: 0, Lat: 3},
		{Lon: 0, Lat: 0},
	}
	c := Centroid(tri)
	if math.Abs(c.Lon-1.0) > 1e-9 || math.Abs(c.Lat-1.0) > 1e-9 {
		t.Errorf("Centroid(triangle) = %+v, want (1,1)", c)
	}
}

func TestCentroid_DegenerateFallsBackToMean(t *testing.T) {
	// Collinear ring → Shoelace area == 0 → vertex-mean fallback.
	col := Polygon{
		{Lon: 0, Lat: 0},
		{Lon: 1, Lat: 1},
		{Lon: 2, Lat: 2},
	}
	c := Centroid(col)
	if math.Abs(c.Lon-1.0) > 1e-9 || math.Abs(c.Lat-1.0) > 1e-9 {
		t.Errorf("Centroid(collinear) = %+v, want (1,1) via fallback", c)
	}
}

func TestMultiPolygonAreaM2_SumsMembers(t *testing.T) {
	a := squareAtLat(0, 45, 100)
	b := squareAtLat(0.01, 45, 50)
	mp := MultiPolygon{a, b}
	got := MultiPolygonAreaM2(mp)
	want := 10000.0 + 2500.0
	relErr := math.Abs(got-want) / want
	if relErr > 0.005 {
		t.Errorf("MultiPolygonAreaM2 = %.2f, want %.2f (relErr %.4f)", got, want, relErr)
	}
}

func TestMultiPolygonCentroid_FirstPolygon(t *testing.T) {
	a := squareAtLat(2.35, 48.85, 100)
	b := squareAtLat(0.0, 0.0, 100)
	mp := MultiPolygon{a, b}
	c := MultiPolygonCentroid(mp)
	if math.Abs(c.Lon-2.35) > 1e-6 || math.Abs(c.Lat-48.85) > 1e-6 {
		t.Errorf("MultiPolygonCentroid = %+v, want (2.35, 48.85)", c)
	}
}
