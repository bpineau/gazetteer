package qpv

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/geopoly"
)

func ptr(f float64) *float64 { return &f }

// square returns a single-polygon MultiPolygon covering the axis-aligned
// box [minLon,maxLon] x [minLat,maxLat].
func square(minLon, minLat, maxLon, maxLat float64) geopoly.MultiPolygon {
	return geopoly.MultiPolygon{geopoly.Polygon{geopoly.Ring{
		{Lon: minLon, Lat: minLat},
		{Lon: minLon, Lat: maxLat},
		{Lon: maxLon, Lat: maxLat},
		{Lon: maxLon, Lat: minLat},
		{Lon: minLon, Lat: minLat},
	}}}
}

// testIndex builds an Index with one QPV square in Paris (75056) plus one in
// Saint-Denis (93066), via the test seam.
func testIndex() *Index {
	return NewIndexForTest([]FeatureForTest{
		{Code: "QN07511M", Label: "Goutte D'Or", INSEE: []string{"75056"},
			Polygons: square(2.34, 48.88, 2.36, 48.89)},
		{Code: "QN09301M", Label: "Test SD", INSEE: []string{"93066"},
			Polygons: square(2.35, 48.93, 2.37, 48.94)},
	})
}

// TestQuery_PointInside proves a coordinate inside a QPV square resolves to
// that single QPV at point granularity, high confidence.
func TestQuery_PointInside(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "75110", Lat: ptr(48.885), Lon: ptr(2.35)}
	res, err := Query(context.Background(), Options{Index: testIndex()}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !res.HasQPV {
		t.Fatalf("HasQPV = false, want true (point inside Goutte d'Or)")
	}
	if res.MatchLevel != MatchLevelPoint {
		t.Errorf("MatchLevel = %q, want %q", res.MatchLevel, MatchLevelPoint)
	}
	if res.QPVCount != 1 || len(res.QPVs) != 1 {
		t.Fatalf("QPVCount = %d, len(QPVs) = %d, want 1/1", res.QPVCount, len(res.QPVs))
	}
	if res.QPVs[0].Code != "QN07511M" {
		t.Errorf("Code = %q, want QN07511M", res.QPVs[0].Code)
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
}

// TestQuery_PointOutside_RegressionParis10e is THE bug fix: an address in a
// commune that hosts QPVs, but whose coordinates fall outside every polygon,
// must answer HasQPV=false (count 0) at high confidence — not the whole
// commune's list.
func TestQuery_PointOutside_RegressionParis10e(t *testing.T) {
	t.Parallel()
	// Paris 10e centroid-ish, far from the Goutte d'Or square.
	l := gazetteer.Listing{INSEE: "75110", Lat: ptr(48.873128), Lon: ptr(2.353599)}
	res, err := Query(context.Background(), Options{Index: testIndex()}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.HasQPV {
		t.Errorf("HasQPV = true, want false (Paris 10e address is not in any QPV)")
	}
	if res.QPVCount != 0 {
		t.Errorf("QPVCount = %d, want 0 (regression: must not return all 21 Paris QPV)", res.QPVCount)
	}
	if res.MatchLevel != MatchLevelPoint {
		t.Errorf("MatchLevel = %q, want %q", res.MatchLevel, MatchLevelPoint)
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q (an outside-all answer is high-confidence)", res.Confidence, ConfidenceHigh)
	}
	if !res.IsEmpty() {
		t.Errorf("IsEmpty = false, want true")
	}
}

// TestQuery_CommuneFallback proves the no-coordinates path returns the
// commune's QPV list at medium confidence and commune match level.
func TestQuery_CommuneFallback(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "93066"} // no lat/lon
	res, err := Query(context.Background(), Options{Index: testIndex()}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !res.HasQPV {
		t.Fatalf("HasQPV = false, want true (Saint-Denis hosts a QPV)")
	}
	if res.MatchLevel != MatchLevelCommune {
		t.Errorf("MatchLevel = %q, want %q", res.MatchLevel, MatchLevelCommune)
	}
	if res.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceMedium)
	}
	if res.QPVCount != 1 {
		t.Errorf("QPVCount = %d, want 1", res.QPVCount)
	}
}

// TestQuery_CommuneFallback_FoldsArrondissement proves a Paris arrondissement
// INSEE folds to 75056 on the commune path.
func TestQuery_CommuneFallback_FoldsArrondissement(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "75118"} // Paris 18e, no coords
	res, err := Query(context.Background(), Options{Index: testIndex()}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !res.HasQPV || res.MatchLevel != MatchLevelCommune {
		t.Fatalf("got HasQPV=%t MatchLevel=%q, want true/commune", res.HasQPV, res.MatchLevel)
	}
	if res.Evidence.INSEE != "75056" {
		t.Errorf("Evidence.INSEE = %q, want 75056 (folded)", res.Evidence.INSEE)
	}
}

// TestQuery_Nearest exercises the nearest-QPV hint: a point just outside a
// polygon, within NearestQPVMaxMeters, records the nearest QPV without
// flipping HasQPV.
func TestQuery_Nearest(t *testing.T) {
	t.Parallel()
	// A point ~440 m east of the Goutte d'Or square's east edge (2.36),
	// at the same latitude band; still outside.
	l := gazetteer.Listing{INSEE: "75110", Lat: ptr(48.885), Lon: ptr(2.366)}
	res, err := Query(context.Background(), Options{Index: testIndex()}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.HasQPV {
		t.Fatalf("HasQPV = true, want false (point is outside, nearest is only a hint)")
	}
	if res.NearestCode != "QN07511M" {
		t.Errorf("NearestCode = %q, want QN07511M", res.NearestCode)
	}
	if res.NearestMeters <= 0 || res.NearestMeters > NearestQPVMaxMeters {
		t.Errorf("NearestMeters = %f, want (0, %f]", res.NearestMeters, NearestQPVMaxMeters)
	}
}

// TestQuery_InsufficientInputs rejects empty INSEE.
func TestQuery_InsufficientInputs(t *testing.T) {
	t.Parallel()
	_, err := Query(context.Background(), Options{Index: testIndex()}, gazetteer.Listing{})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Fatalf("err = %v, want ErrInsufficientInputs", err)
	}
}

// TestSourceRegistered ensures the init() side-effect wired the registry.
func TestSourceRegistered(t *testing.T) {
	t.Parallel()
	if got := gazetteer.Lookup(Name); got == nil {
		t.Fatalf("gazetteer.Lookup(%q) = nil, want factory", Name)
	}
}
