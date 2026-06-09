package overview

import (
	"strings"
	"testing"
)

func TestBuild_KnownCommune(t *testing.T) {
	t.Parallel()

	rows, err := Build(Options{Depts: []string{"95"}})
	if err != nil {
		t.Fatal(err)
	}
	var g *CommuneOverview
	for i := range rows {
		if rows[i].INSEE == "95268" {
			g = &rows[i]
		}
	}
	if g == nil {
		t.Fatal("95268 absent from Build results")
	}
	if g.PriceMedianSmallEURM2 <= 0 || g.RentMarketEURM2HC <= 0 || g.Dept != "95" {
		t.Fatalf("incomplete row: %+v", g)
	}
	t.Logf("95268: price_small=%.0f rent_hc=%.2f zonage=%s tendue=%s delinquance=%s qpv=%v",
		g.PriceMedianSmallEURM2, g.RentMarketEURM2HC, g.ZonageABC, g.ZoneTendue, g.DelinquanceLevel, g.QPV)
}

// ---------------------------------------------------------------------------
// DistanceParisKm
// ---------------------------------------------------------------------------

// TestDistanceParisKm_Versailles checks that a well-known outer commune
// (Versailles, 78646, ~18 km from Notre-Dame) gets a plausible value.
func TestDistanceParisKm_Versailles(t *testing.T) {
	t.Parallel()

	rows, err := Build(Options{Depts: []string{"78"}})
	if err != nil {
		t.Fatal(err)
	}
	var g *CommuneOverview
	for i := range rows {
		if rows[i].INSEE == "78646" {
			g = &rows[i]
		}
	}
	if g == nil {
		t.Skip("78646 (Versailles) absent from embedded DVF data — skip")
	}
	if g.DistanceParisKm <= 0 {
		t.Errorf("DistanceParisKm = %.1f, want > 0", g.DistanceParisKm)
	}
	if g.DistanceParisKm > 120 {
		t.Errorf("DistanceParisKm = %.1f, suspiciously large for a 78 commune", g.DistanceParisKm)
	}
	t.Logf("Versailles 78646: DistanceParisKm=%.1f", g.DistanceParisKm)
}

// TestDistanceParisKm_AllRows ensures every row produced by Build has a
// non-negative DistanceParisKm and that IDF communes stay within a
// reasonable bound (all IDF depts are <80 km from Paris).
func TestDistanceParisKm_AllRows(t *testing.T) {
	t.Parallel()

	rows, err := Build(Options{Depts: []string{"75", "77", "78", "91", "92", "93", "94", "95"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.DistanceParisKm < 0 {
			t.Errorf("%s: DistanceParisKm = %.1f, want >= 0", r.INSEE, r.DistanceParisKm)
		}
		// Seine-et-Marne (77) is the largest IDF dept and reaches ~85 km at
		// its far eastern tip; 100 km is a safe ceiling for the whole region.
		if r.DistanceParisKm > 100 {
			t.Errorf("%s: DistanceParisKm = %.1f, want <= 100 for IDF commune", r.INSEE, r.DistanceParisKm)
		}
	}
}

// ---------------------------------------------------------------------------
// TransitLines
// ---------------------------------------------------------------------------

// TestTransitLines_SaintDenis checks that Saint-Denis (93066), which sits
// on Métro 13, RER B/D and several tram lines, returns a non-empty slice
// containing at least one recognisable label.
func TestTransitLines_SaintDenis(t *testing.T) {
	t.Parallel()

	rows, err := Build(Options{Depts: []string{"93"}})
	if err != nil {
		t.Fatal(err)
	}
	var g *CommuneOverview
	for i := range rows {
		if rows[i].INSEE == "93066" {
			g = &rows[i]
		}
	}
	if g == nil {
		t.Skip("93066 (Saint-Denis) absent from embedded DVF data — skip")
	}
	if len(g.TransitLines) == 0 {
		t.Fatal("TransitLines is empty for Saint-Denis — expected rail/metro/rer service")
	}
	// At least one label should contain a recognisable transit keyword.
	hasKnown := false
	for _, lbl := range g.TransitLines {
		if strings.Contains(lbl, "Métro") || strings.Contains(lbl, "RER") ||
			strings.Contains(lbl, "Transilien") || strings.HasPrefix(lbl, "T") || strings.Contains(lbl, "Train") {
			hasKnown = true
			break
		}
	}
	if !hasKnown {
		t.Errorf("TransitLines = %v — none of the labels look like a transit line", g.TransitLines)
	}
	t.Logf("93066 Saint-Denis TransitLines: %v", g.TransitLines)
}

// TestTransitLines_RuralCommune ensures that a distant rural commune (if
// present in the DVF data) has an empty or nil TransitLines slice.
// We use a low-density dept like 77 and accept the test as "at least one
// commune with nil/empty TransitLines exists", not every commune.
func TestTransitLines_Cap(t *testing.T) {
	t.Parallel()

	rows, err := Build(Options{Depts: []string{"77", "78", "91", "93", "94", "95"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if len(r.TransitLines) > maxTransitLines {
			t.Errorf("%s: len(TransitLines)=%d > cap %d", r.INSEE, len(r.TransitLines), maxTransitLines)
		}
	}
}

func TestBuild_NoDeptFilter(t *testing.T) {
	t.Parallel()

	rows, err := Build(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("Build with no dept filter returned empty slice")
	}
	// All rows should have a non-empty Dept and non-zero price.
	for _, r := range rows {
		if r.Dept == "" {
			t.Errorf("row %s has empty Dept", r.INSEE)
		}
		if r.PriceMedianEURM2 <= 0 {
			t.Errorf("row %s has PriceMedianEURM2 = %.2f, want > 0", r.INSEE, r.PriceMedianEURM2)
		}
	}
}
