package cadastre

import (
	"errors"
	"testing"
)

func TestParseFeatureCollection_Paris1er(t *testing.T) {
	t.Parallel()

	fc, err := ParseFeatureCollection(mustReadFixture(t, "parcelle_paris_1er.json"))
	if err != nil {
		t.Fatalf("ParseFeatureCollection: %v", err)
	}
	if len(fc.Features) == 0 {
		t.Fatal("no features in paris1er fixture")
	}
	p := fc.Features[0].Properties
	if p.CodeInsee != "75056" {
		t.Errorf("CodeInsee = %q, want 75056", p.CodeInsee)
	}
	if p.IDU == "" {
		t.Error("IDU is empty — paris1er fixture is expected to carry one")
	}
	if p.Contenance <= 0 {
		t.Errorf("Contenance = %d, want >0", p.Contenance)
	}
	// Spot-check section / numero are non-empty.
	if p.Section == "" || p.Numero == "" {
		t.Errorf("Section/Numero empty: section=%q numero=%q", p.Section, p.Numero)
	}
}

func TestParseFeatureCollection_SmallCommune(t *testing.T) {
	t.Parallel()

	fc, err := ParseFeatureCollection(mustReadFixture(t, "parcelle_small_commune.json"))
	if err != nil {
		t.Fatalf("ParseFeatureCollection: %v", err)
	}
	if len(fc.Features) == 0 {
		t.Fatal("no features in small_commune fixture")
	}
	p := fc.Features[0].Properties
	if p.CodeInsee == "" || len(p.CodeInsee) != 5 {
		t.Errorf("CodeInsee = %q, want 5-char", p.CodeInsee)
	}
	if p.IDU != "" && len(p.IDU) != 14 {
		t.Errorf("IDU = %q, want 14-char Etalab id", p.IDU)
	}
}

func TestParseFeatureCollection_Empty(t *testing.T) {
	t.Parallel()

	fc, err := ParseFeatureCollection(mustReadFixture(t, "parcelle_empty.json"))
	if err != nil {
		t.Fatalf("ParseFeatureCollection(empty): %v", err)
	}
	if len(fc.Features) != 0 {
		t.Errorf("len(Features) = %d, want 0", len(fc.Features))
	}
}

func TestParseFeatureCollection_EmptyBody(t *testing.T) {
	t.Parallel()

	_, err := ParseFeatureCollection(nil)
	if !errors.Is(err, ErrEmptyBody) {
		t.Fatalf("ParseFeatureCollection(nil) = %v, want ErrEmptyBody", err)
	}
}

func TestParseFeatureCollection_Garbage(t *testing.T) {
	t.Parallel()

	_, err := ParseFeatureCollection([]byte("not json"))
	if !errors.Is(err, ErrEmptyBody) {
		t.Fatalf("ParseFeatureCollection(garbage) = %v, want ErrEmptyBody wrap", err)
	}
}

func TestParsePolygonGeometry_MultiPolygon(t *testing.T) {
	t.Parallel()

	fc, err := ParseFeatureCollection(mustReadFixture(t, "parcelle_paris_1er.json"))
	if err != nil {
		t.Fatalf("ParseFeatureCollection: %v", err)
	}
	mp, err := ParsePolygonGeometry(fc.Features[0].Geometry)
	if err != nil {
		t.Fatalf("ParsePolygonGeometry: %v", err)
	}
	if len(mp) == 0 {
		t.Fatal("decoded MultiPolygon is empty")
	}
	if len(mp[0]) != 1 {
		t.Fatalf("decoded polygon has %d rings, want 1 (outer only)", len(mp[0]))
	}
	if len(mp[0][0]) < 3 {
		t.Errorf("outer ring has %d points, want >=3", len(mp[0][0]))
	}
}

func TestParsePolygonGeometry_BarePolygon(t *testing.T) {
	t.Parallel()

	// Synthetic schema-drift case: upstream returns a bare Polygon
	// geometry instead of MultiPolygon. We accept it as a defensive
	// fallback.
	g := RawGeometry{
		Type:        "Polygon",
		Coordinates: []byte(`[[[0,0],[1,0],[1,1],[0,1],[0,0]]]`),
	}
	mp, err := ParsePolygonGeometry(g)
	if err != nil {
		t.Fatalf("ParsePolygonGeometry: %v", err)
	}
	if len(mp) != 1 || len(mp[0]) != 1 || len(mp[0][0]) != 5 {
		t.Errorf("MultiPolygon shape = %d polygons / %d rings / %d points, want 1/1/5",
			len(mp), len(mp[0]), len(mp[0][0]))
	}
}

func TestParsePolygonGeometry_RejectUnsupported(t *testing.T) {
	t.Parallel()

	g := RawGeometry{Type: "LineString", Coordinates: []byte(`[[0,0],[1,1]]`)}
	if _, err := ParsePolygonGeometry(g); err == nil {
		t.Error("ParsePolygonGeometry(LineString) want err, got nil")
	}
}

func TestPickFeature_ContainmentHit(t *testing.T) {
	t.Parallel()

	fc, err := ParseFeatureCollection(mustReadFixture(t, "parcelle_paris_1er.json"))
	if err != nil {
		t.Fatalf("ParseFeatureCollection: %v", err)
	}
	pick, ok := PickFeature(fc.Features, 2.3522, 48.8566)
	if !ok || pick.Index < 0 {
		t.Fatalf("PickFeature = (%+v, %v), want a hit", pick, ok)
	}
	if !pick.Contains {
		t.Errorf("Contains = false, want true: the point is inside the parcel")
	}
	if pick.DistanceM != 0 {
		t.Errorf("DistanceM = %v, want 0 on a containment hit", pick.DistanceM)
	}
}

func TestPickFeature_FallbackToFirstWhenNoneContain(t *testing.T) {
	t.Parallel()

	fc, err := ParseFeatureCollection(mustReadFixture(t, "parcelle_paris_1er.json"))
	if err != nil {
		t.Fatalf("ParseFeatureCollection: %v", err)
	}
	// Way outside every parcel: no containment hit. The nearest one is
	// still returned, but it says so and says how far — the fallback
	// used to be feature 0 with an "ok" that meant nothing, so a parcel
	// 700 km away read exactly like a hit.
	pick, ok := PickFeature(fc.Features, 10.0, 50.0)
	if !ok {
		t.Fatalf("PickFeature(out-of-range) ok = false, want true")
	}
	if pick.Contains {
		t.Errorf("Contains = true, want false: the point is nowhere near")
	}
	if pick.DistanceM < 500_000 {
		t.Errorf("DistanceM = %.0f m, want the real (huge) distance to Paris", pick.DistanceM)
	}
}

func TestPickFeature_EmptyList(t *testing.T) {
	t.Parallel()

	pick, ok := PickFeature(nil, 0, 0)
	if ok || pick.Index != -1 {
		t.Errorf("PickFeature(nil) = (%+v, %v), want (Index -1, false)", pick, ok)
	}
}
