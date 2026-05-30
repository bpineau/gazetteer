package encadrement

import (
	"context"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestLoad_93 smokes the Est Ensemble barème and the zonage geometry.
func TestLoad_93(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := idx.CountEstEnsemble(); got < 100 {
		t.Errorf("CountEstEnsemble = %d, want ≥ 100", got)
	}
	// 9 PC communes (Saint-Denis ×2) + 9 EE communes (Montreuil ×2) = 20 zones.
	if got := len(idx.zones); got != 20 {
		t.Errorf("loaded zones = %d, want 20", got)
	}
	// Saint-Denis (93066) straddles two PC zones; Pantin (93055) is single-zone.
	if got := idx.inseeZones["93066"]; len(got) != 2 {
		t.Errorf("Saint-Denis zones = %v, want 2", got)
	}
	if got := idx.inseeZones["93055"]; len(got) != 1 {
		t.Errorf("Pantin zones = %v, want 1", got)
	}
}

// TestQuery_PlaineCommune_PointInPolygon resolves a Saint-Denis coordinate to
// its sub-communal zone.
func TestQuery_PlaineCommune_PointInPolygon(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{
		INSEE:        "93066",
		Lat:          new(48.9355), // Basilique de Saint-Denis → PC zone 311
		Lon:          new(2.3590),
		PropertyType: gazetteer.PropertyApartment,
		Rooms:        new(2),
	}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Saint-Denis")
	}
	if res.ZoneSource != ZoneSourcePlaineCommune {
		t.Errorf("ZoneSource = %q, want %q", res.ZoneSource, ZoneSourcePlaineCommune)
	}
	if res.Zone != "Saint-Denis" {
		t.Errorf("Zone = %q, want Saint-Denis", res.Zone)
	}
	if res.Evidence.ZoneID != "311" {
		t.Errorf("Evidence.ZoneID = %q, want 311", res.Evidence.ZoneID)
	}
	if res.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceMedium)
	}
	if res.LoyerRefMajEURPerM2HC < 8 || res.LoyerRefMajEURPerM2HC > 40 {
		t.Errorf("LoyerRefMajEURPerM2HC = %.2f, want in [8, 40]", res.LoyerRefMajEURPerM2HC)
	}
}

// TestQuery_EstEnsemble_PointInPolygon resolves a Montreuil coordinate.
func TestQuery_EstEnsemble_PointInPolygon(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{
		INSEE:        "93048",
		Lat:          new(48.8627), // Mairie de Montreuil → EE zone 307
		Lon:          new(2.4436),
		PropertyType: gazetteer.PropertyApartment,
		Rooms:        new(3),
	}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Montreuil")
	}
	if res.ZoneSource != ZoneSourceEstEnsemble {
		t.Errorf("ZoneSource = %q, want %q", res.ZoneSource, ZoneSourceEstEnsemble)
	}
	if res.Zone != "Montreuil" {
		t.Errorf("Zone = %q, want Montreuil", res.Zone)
	}
	if res.Evidence.ZoneID != "307" {
		t.Errorf("Evidence.ZoneID = %q, want 307", res.Evidence.ZoneID)
	}
	if res.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceMedium)
	}
}

// TestQuery_EstEnsemble_DisjointRingBody resolves a coordinate in the main body
// of Montreuil zone 308, which the upstream encodes as a ring lying outside
// ring 0's bounding box. Regression guard: the geometry bbox prefilter must span
// every ring, else the point is wrongly rejected and falls back to the ambiguous
// multi-zone path (307+312/low) instead of resolving to 308/medium.
func TestQuery_EstEnsemble_DisjointRingBody(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{
		INSEE:        "93048",
		Lat:          new(48.87201),
		Lon:          new(2.43316),
		PropertyType: gazetteer.PropertyApartment,
		Rooms:        new(2),
	}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Evidence.ZoneID != "308" {
		t.Errorf("Evidence.ZoneID = %q, want 308 (body of a disjoint-ring zone)", res.Evidence.ZoneID)
	}
	if res.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceMedium)
	}
}

// TestQuery_EstEnsemble_InseeFallback_SingleZone resolves a single-zone EPT
// commune without coordinates at medium confidence.
func TestQuery_EstEnsemble_InseeFallback_SingleZone(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{
		INSEE:        "93055", // Pantin — single EE zone 308
		PropertyType: gazetteer.PropertyApartment,
		Rooms:        new(2),
	}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Pantin (no coords)")
	}
	if res.ZoneSource != ZoneSourceEstEnsemble {
		t.Errorf("ZoneSource = %q, want %q", res.ZoneSource, ZoneSourceEstEnsemble)
	}
	if res.Evidence.ZoneID != "308" {
		t.Errorf("Evidence.ZoneID = %q, want 308", res.Evidence.ZoneID)
	}
	if res.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want %q (single-zone commune)", res.Confidence, ConfidenceMedium)
	}
}

// TestQuery_PlaineCommune_InseeFallback_MultiZone collapses a multi-zone commune
// queried without coordinates at low confidence.
func TestQuery_PlaineCommune_InseeFallback_MultiZone(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{
		INSEE:        "93066", // Saint-Denis — zones 311 + 312, no coords → ambiguous
		PropertyType: gazetteer.PropertyApartment,
		Rooms:        new(2),
	}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Saint-Denis (no coords)")
	}
	if res.Confidence != ConfidenceLow {
		t.Errorf("Confidence = %q, want %q (ambiguous multi-zone)", res.Confidence, ConfidenceLow)
	}
	if res.Evidence.ZoneID != "311+312" {
		t.Errorf("Evidence.ZoneID = %q, want 311+312", res.Evidence.ZoneID)
	}
}

// TestQuery_93_PrefersPointOverFallback ensures a multi-zone commune WITH a
// coordinate resolves to the precise zone (medium), not the ambiguous fallback.
func TestQuery_93_PrefersPointOverFallback(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{
		INSEE:        "93066",
		Lat:          new(48.9190), // Pleyel, southern Saint-Denis → zone 311
		Lon:          new(2.3430),
		PropertyType: gazetteer.PropertyApartment,
		Rooms:        new(1),
	}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want %q (point resolves precise zone)", res.Confidence, ConfidenceMedium)
	}
	if res.Evidence.ZoneID == "311+312" {
		t.Errorf("Evidence.ZoneID = %q, want a single zone (point-in-polygon)", res.Evidence.ZoneID)
	}
}
