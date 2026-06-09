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
	idx, ok := PickFeature(fc.Features, 2.3522, 48.8566)
	if !ok || idx < 0 {
		t.Fatalf("PickFeature = (%d, %v), want a hit", idx, ok)
	}
}

func TestPickFeature_FallbackToFirstWhenNoneContain(t *testing.T) {
	t.Parallel()

	fc, err := ParseFeatureCollection(mustReadFixture(t, "parcelle_paris_1er.json"))
	if err != nil {
		t.Fatalf("ParseFeatureCollection: %v", err)
	}
	// Way outside the parcel — no containment hit, fallback to first.
	idx, ok := PickFeature(fc.Features, 10.0, 50.0)
	if !ok || idx != 0 {
		t.Errorf("PickFeature(out-of-range) = (%d, %v), want (0, true)", idx, ok)
	}
}

func TestPickFeature_EmptyList(t *testing.T) {
	t.Parallel()

	idx, ok := PickFeature(nil, 0, 0)
	if ok || idx != -1 {
		t.Errorf("PickFeature(nil) = (%d, %v), want (-1, false)", idx, ok)
	}
}
