package encadrement

import (
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// quartierCap returns the published majoré of one Paris quartier's cell, read
// straight from the embedded barème, so the test follows a vintage refresh.
func quartierCap(t *testing.T, arr, zoneID string, piece int, epoque string) float64 {
	t.Helper()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for _, e := range idx.LookupParis(arr) {
		if e.ZoneID == zoneID && e.Piece == piece && e.Epoque == epoque && !e.Meuble && !e.Maison {
			return e.LoyerRefMaxEURPerM2HC
		}
	}
	t.Fatalf("no cell %s/%d/%s in the embedded barème", zoneID, piece, epoque)
	return 0
}

// Paris sets its caps per QUARTIER (80 of them, grouped into 14 secteurs), not
// per arrondissement, and an arrondissement can straddle four secteurs. The
// Paris IRIS code carries the quartier (751 + arrondissement + quartier + n),
// so a listing whose IRIS is known must read its own quartier's cell.
//
// Before: Paris 13e, 3 rooms, built 1980, IRIS in quartier Gare (secteur 13)
// read the median over the four 13e quartiers, 27.15 EUR/m2/month against the
// quartier's own 19.80 on the 2025 grille: 37 % above the legal ceiling, with
// Confidence medium.
func TestQuery_ParisIRISPicksTheQuartier(t *testing.T) {
	t.Parallel()
	const gare = "7511350"
	want := quartierCap(t, "13", gare, 3, "1971-1990")

	res := query(t, gazetteer.Listing{INSEE: "75113", IRIS: "751135001", Rooms: intp(3), BuildYear: intp(1980)})
	if res.LoyerRefMajEURPerM2HC != want {
		t.Errorf("maj = %.2f, want the Gare quartier's own %.2f", res.LoyerRefMajEURPerM2HC, want)
	}
	if res.Evidence.ZoneID != gare {
		t.Errorf("Evidence.ZoneID = %q, want %q", res.Evidence.ZoneID, gare)
	}
	if res.Confidence != ConfidenceMedium {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceMedium)
	}
}

// Without the quartier, an arrondissement spanning several secteurs can only
// be collapsed across them: that is the same ambiguity as a multi-zone 93
// commune queried without coordinates, and it carries the same Low confidence.
// An arrondissement inside one secteur (the 3e) is unambiguous and stays
// Medium.
func TestQuery_ParisArrondissementAmbiguity(t *testing.T) {
	t.Parallel()
	if res := query(t, gazetteer.Listing{INSEE: "75113", Rooms: intp(3), BuildYear: intp(1980)}); res.Confidence != ConfidenceLow {
		t.Errorf("13e without IRIS: Confidence = %q, want %q (four secteurs collapsed)", res.Confidence, ConfidenceLow)
	}
	if res := query(t, gazetteer.Listing{INSEE: "75103", Rooms: intp(3), BuildYear: intp(1980)}); res.Confidence != ConfidenceMedium {
		t.Errorf("3e without IRIS: Confidence = %q, want %q (one secteur)", res.Confidence, ConfidenceMedium)
	}
}

// An IRIS that names another arrondissement, or no Paris quartier at all, must
// not pin a quartier: the arrondissement reading stands.
func TestQuery_ParisForeignIRISIgnored(t *testing.T) {
	t.Parallel()
	base := query(t, gazetteer.Listing{INSEE: "75113", Rooms: intp(3), BuildYear: intp(1980)})
	for _, iris := range []string{"751010101", "930660101", "75113", "7511350xx"} {
		res := query(t, gazetteer.Listing{INSEE: "75113", IRIS: iris, Rooms: intp(3), BuildYear: intp(1980)})
		if res.LoyerRefMajEURPerM2HC != base.LoyerRefMajEURPerM2HC || res.Evidence.ZoneID != "" {
			t.Errorf("IRIS %q: maj = %.2f zone %q, want the arrondissement reading %.2f", iris,
				res.LoyerRefMajEURPerM2HC, res.Evidence.ZoneID, base.LoyerRefMajEURPerM2HC)
		}
	}
}

// Lyon zones its grille per IRIS too. A caller-supplied IRIS pins its own
// cells; without one, a Lyon 3e collapse spans five zones (12.50 to 16.30
// EUR/m2/month on the same cell on the 2025-2026 grille) and is Low.
func TestQuery_LyonIRISPicksTheZone(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	const iris = "693830101"
	var want float64
	for _, e := range idx.LookupLyonIRIS(iris) {
		if e.Piece == 3 && e.Epoque == "1971-1990" && !e.Meuble {
			want = e.LoyerRefMaxEURPerM2HC
		}
	}
	if want == 0 {
		t.Fatalf("no 3-room 1971-1990 cell for IRIS %s", iris)
	}

	res := query(t, gazetteer.Listing{INSEE: "69383", IRIS: iris, Rooms: intp(3), BuildYear: intp(1980)})
	if res.LoyerRefMajEURPerM2HC != want || res.Evidence.ZoneID != iris || res.Confidence != ConfidenceMedium {
		t.Errorf("with IRIS: maj %.2f zone %q conf %q, want %.2f %q %q",
			res.LoyerRefMajEURPerM2HC, res.Evidence.ZoneID, res.Confidence, want, iris, ConfidenceMedium)
	}
	if res := query(t, gazetteer.Listing{INSEE: "69383", Rooms: intp(3), BuildYear: intp(1980)}); res.Confidence != ConfidenceLow {
		t.Errorf("without IRIS: Confidence = %q, want %q (five zones collapsed)", res.Confidence, ConfidenceLow)
	}
}
