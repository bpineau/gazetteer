package cadastre

import (
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/helpers/geopoly"
)

func TestParseBatiFeatureCollection_Synthetic(t *testing.T) {
	t.Parallel()

	body := mustReadFixture(t, "batiments_small.json")
	fc, err := ParseBatiFeatureCollection(body)
	if err != nil {
		t.Fatalf("ParseBatiFeatureCollection: %v", err)
	}
	if got, want := len(fc.Features), 4; got != want {
		t.Errorf("len(features) = %d, want %d", got, want)
	}
}

func TestParseBatiFeatureCollection_EmptyBody(t *testing.T) {
	t.Parallel()

	if _, err := ParseBatiFeatureCollection(nil); !errors.Is(err, ErrEmptyBody) {
		t.Errorf("ParseBatiFeatureCollection(nil) = %v, want ErrEmptyBody", err)
	}
}

func TestLoadBatiPolygons_PrecomputesCentroidAndArea(t *testing.T) {
	t.Parallel()

	body := mustReadFixture(t, "batiments_small.json")
	polys, raw, err := LoadBatiPolygons(body)
	if err != nil {
		t.Fatalf("LoadBatiPolygons: %v", err)
	}
	if raw != 4 {
		t.Errorf("raw count = %d, want 4", raw)
	}
	if len(polys) != 4 {
		t.Errorf("len(polys) = %d, want 4", len(polys))
	}
	for i, p := range polys {
		if len(p.Geometry) == 0 {
			t.Errorf("polys[%d] has empty geometry", i)
		}
		if p.AreaM2 <= 0 {
			t.Errorf("polys[%d].AreaM2 = %v, want >0", i, p.AreaM2)
		}
		// Centroid must lie inside the polygon — sanity for the
		// downstream PIP filter.
		if !p.Geometry.Covers(p.Centroid) {
			t.Errorf("polys[%d] centroid %+v not inside its own geometry", i, p.Centroid)
		}
	}
}

// TestFilterBatiInParcel_CentroidPIP exercises the centroid-PIP filter:
// 2 polygons are inside a synthetic parcel polygon, 2 outside.
func TestFilterBatiInParcel_CentroidPIP(t *testing.T) {
	t.Parallel()

	body := mustReadFixture(t, "batiments_small.json")
	polys, _, err := LoadBatiPolygons(body)
	if err != nil {
		t.Fatalf("LoadBatiPolygons: %v", err)
	}

	// Parcel polygon enclosing the first two bâti centroids
	// (around 2.0001/49.0001 + 2.0003/49.0001), excluding the last two
	// (at 2.01.../49.01... and 2.02.../49.02...).
	parcel := geopoly.MultiPolygon{
		geopoly.Polygon{
			geopoly.Ring{
				{Lon: 1.9999, Lat: 48.9999},
				{Lon: 2.0004, Lat: 48.9999},
				{Lon: 2.0004, Lat: 49.0002},
				{Lon: 1.9999, Lat: 49.0002},
				{Lon: 1.9999, Lat: 48.9999},
			},
		},
	}
	got := filterBatiInParcel(polys, parcel)
	if len(got) != 2 {
		t.Errorf("filterBatiInParcel kept %d, want 2", len(got))
	}
	total := sumBatiArea(got)
	if total <= 0 {
		t.Errorf("sumBatiArea = %v, want >0", total)
	}
}

func TestFilterBatiInParcel_EmptyInputs(t *testing.T) {
	t.Parallel()

	if got := filterBatiInParcel(nil, geopoly.MultiPolygon{}); got != nil {
		t.Errorf("filterBatiInParcel(nil, empty) = %v, want nil", got)
	}
}
