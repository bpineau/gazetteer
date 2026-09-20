package cadastre

import (
	"math"
	"testing"

	"github.com/bpineau/gazetteer/helpers/geopoly"
)

// rect is a rectangular ring from (lon0, lat0) to (lon1, lat1).
func rect(lon0, lat0, lon1, lat1 float64) geopoly.Ring {
	return geopoly.Ring{
		{Lon: lon0, Lat: lat0}, {Lon: lon1, Lat: lat0},
		{Lon: lon1, Lat: lat1}, {Lon: lon0, Lat: lat1},
	}
}

// parcelMP is the synthetic parcel both tests sit on.
func parcelMP() geopoly.MultiPolygon {
	return geopoly.MultiPolygon{geopoly.Polygon{rect(2.3500, 48.8500, 2.3510, 48.8505)}}
}

// batiFrom builds the cached shape the loader would produce for mp.
func batiFrom(t *testing.T, mp geopoly.MultiPolygon) BatiPolygon {
	t.Helper()
	parts := make([]BatiPart, 0, len(mp))
	for _, poly := range mp {
		inside, ok := poly.RepresentativePoint()
		if !ok {
			t.Fatalf("no representative point for %+v", poly)
		}
		parts = append(parts, BatiPart{Inside: inside, AreaM2: poly.AreaM2()})
	}
	return BatiPolygon{Geometry: mp, Parts: parts}
}

// TestFilterBatiInParcel_MultiPartIsOrderIndependent is the regression for the
// first-part-centroid bug: containment was decided on the FIRST member
// polygon's centroid while the WHOLE feature's area was credited, so a
// building with one wing on the parcel and one outside was counted entirely or
// not at all, depending purely on the order of the wings in the GeoJSON.
func TestFilterBatiInParcel_MultiPartIsOrderIndependent(t *testing.T) {
	t.Parallel()
	parcel := parcelMP()
	wingIn := geopoly.Polygon{rect(2.3501, 48.8501, 2.3504, 48.8504)}
	wingOut := geopoly.Polygon{rect(2.3530, 48.8501, 2.3533, 48.8504)} // ~200 m east

	inFirst := batiFrom(t, geopoly.MultiPolygon{wingIn, wingOut})
	outFirst := batiFrom(t, geopoly.MultiPolygon{wingOut, wingIn})

	_, areaA := filterBatiInParcel([]BatiPolygon{inFirst}, parcel)
	_, areaB := filterBatiInParcel([]BatiPolygon{outFirst}, parcel)

	if math.Abs(areaA-areaB) > 0.01 {
		t.Errorf("area depends on part order: %.1f vs %.1f m²", areaA, areaB)
	}
	// Only the wing on the parcel counts, so the answer is that wing's area,
	// not the pair's.
	want := wingIn.AreaM2()
	if math.Abs(areaA-want) > 0.01 {
		t.Errorf("in-parcel area = %.1f m², want the inside wing's %.1f (whole feature is %.1f)",
			areaA, want, inFirst.AreaM2())
	}
}

// TestFilterBatiInParcel_ConcaveFootprint is the regression for the centroid
// escaping its own shape: an L-shaped building wholly inside its parcel has a
// centroid in the notch, outside both the building and the parcel, so the
// footprint read as zero.
func TestFilterBatiInParcel_ConcaveFootprint(t *testing.T) {
	t.Parallel()
	// A thin L along the parcel's south and west sides. The arms are thin on
	// purpose: the centroid must fall well inside the notch, not on an edge of
	// the L, where the verdict would hang on the platform's floating point.
	l := geopoly.Polygon{geopoly.Ring{
		{Lon: 2.3501, Lat: 48.85010}, {Lon: 2.3509, Lat: 48.85010},
		{Lon: 2.3509, Lat: 48.85015}, {Lon: 2.35015, Lat: 48.85015},
		{Lon: 2.35015, Lat: 48.8504}, {Lon: 2.3501, Lat: 48.8504},
	}}
	mp := geopoly.MultiPolygon{l}
	if mp.Covers(l.Centroid()) {
		t.Fatalf("fixture is not concave: the centroid %+v is inside the L", l.Centroid())
	}

	kept, area := filterBatiInParcel([]BatiPolygon{batiFrom(t, mp)}, parcelMP())
	if len(kept) != 1 {
		t.Fatalf("kept %d buildings, want 1 (the L is wholly inside the parcel)", len(kept))
	}
	if want := l.AreaM2(); math.Abs(area-want) > 0.01 {
		t.Errorf("in-parcel area = %.1f m², want the L's own %.1f", area, want)
	}
}
