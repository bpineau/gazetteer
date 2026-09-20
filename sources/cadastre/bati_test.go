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
		if p.AreaM2() <= 0 {
			t.Errorf("polys[%d].AreaM2() = %v, want >0", i, p.AreaM2())
		}
		if len(p.Parts) != len(p.Geometry) {
			t.Errorf("polys[%d] has %d parts for %d member polygons", i, len(p.Parts), len(p.Geometry))
		}
		// Every part's Inside point must lie inside the geometry — the
		// invariant the whole in-parcel filter rests on.
		for j, part := range p.Parts {
			if !p.Geometry.Covers(part.Inside) {
				t.Errorf("polys[%d] part %d: Inside %+v is not inside its own geometry", i, j, part.Inside)
			}
		}
	}
}

// TestFilterBatiInParcel exercises the in-parcel filter: 2 polygons are
// inside a synthetic parcel polygon, 2 outside.
func TestFilterBatiInParcel(t *testing.T) {
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
	got, total := filterBatiInParcel(polys, parcel)
	if len(got) != 2 {
		t.Errorf("filterBatiInParcel kept %d, want 2", len(got))
	}
	if total <= 0 {
		t.Errorf("in-parcel area = %v, want >0", total)
	}
}

func TestFilterBatiInParcel_EmptyInputs(t *testing.T) {
	t.Parallel()

	if got, area := filterBatiInParcel(nil, geopoly.MultiPolygon{}); got != nil || area != 0 {
		t.Errorf("filterBatiInParcel(nil, empty) = %v, %v; want nil, 0", got, area)
	}
}
