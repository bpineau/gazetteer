package oll

import (
	"strings"
	"testing"
)

// oll rent-table header (superset of the columns parseRents needs).
const reletCSVHeader = "Zone_calcul;Type_habitat;nombre_pieces_local;nombre_pieces_homogene;" +
	"epoque_construction_local;epoque_construction_homogene;" +
	"anciennete_locataire_local;anciennete_locataire_homogene;" +
	"loyer_median;loyer_1_quartile;loyer_3_quartile;surface_moyenne;nombre_observations"

// reletRow builds one rent-table line. anc is the anciennete_locataire_homogene
// value ("" = all-tenancies, olrReletLabel = relet).
func reletRow(zoneCalcul, pieces, anc, median string) string {
	return strings.Join([]string{zoneCalcul, "Appartement", "", pieces, "", "", "", anc, median, "", "", "", "100"}, ";")
}

func findRow(rows []rentRow, zone string, pieces int) (rentRow, bool) {
	for _, r := range rows {
		if r.Zone == zone && r.Pieces == pieces {
			return r, true
		}
	}
	return rentRow{}, false
}

// TestParseRents_Relet exercises the relet ("emménagés récents") logic:
// observed relet is used directly; a per-pièces cell with no observed relet
// inherits the zone-level relet/all ratio; the agglo-wide donor rows are used
// for ratios but not emitted as queryable cells.
func TestParseRents_Relet(t *testing.T) {
	t.Parallel()
	rows := []string{
		reletCSVHeader,
		// Zone 5, all-sizes aggregate: all 17.3, relet 19.5 (ratio ≈ 1.127).
		reletRow("L7502.4.05", "", "", "17,3"),
		reletRow("L7502.4.05", "", olrReletLabel, "19,5"),
		// Zone 5, 2 pièces: all 18.0 only — relet must be derived (18.0 × 1.127).
		reletRow("L7502.4.05", "Appart 2P", "", "18"),
		// Zone 2, all-sizes: all 23.8, relet 26 (ratio ≈ 1.092).
		reletRow("L7502.4.02", "", "", "23,8"),
		reletRow("L7502.4.02", "", olrReletLabel, "26"),
		// Agglo-wide donor rows (Zone_calcul blank): NOT emitted as cells.
		reletRow("", "", "", "18,0"),
		reletRow("", "Appart 2P", "", "17,0"),
		reletRow("", "Appart 2P", olrReletLabel, "18,7"),
	}
	out, err := parseRents(strings.Join(rows, "\n") + "\n")
	if err != nil {
		t.Fatalf("parseRents: %v", err)
	}

	// Agglo-wide rows (zone == "") are donors only.
	for _, r := range out {
		if r.Zone == "" {
			t.Errorf("agglo-wide row leaked as a queryable cell: %+v", r)
		}
	}

	// Zone 5 aggregate: observed relet used verbatim.
	if r, ok := findRow(out, "5", 0); !ok {
		t.Fatal("zone 5 aggregate cell missing")
	} else if r.MedianEURPerM2 != 17.3 || r.ReletMedianEURPerM2 != 19.5 {
		t.Errorf("zone5/p0 = all %.1f relet %.1f, want 17.3 / 19.5 (observed)", r.MedianEURPerM2, r.ReletMedianEURPerM2)
	}

	// Zone 5, 2 pièces: relet derived from the zone ratio 19.5/17.3 ≈ 1.127 →
	// 18.0 × 1.127 = 20.3 (rounded to 1 decimal).
	if r, ok := findRow(out, "5", 2); !ok {
		t.Fatal("zone 5 / 2 pièces cell missing")
	} else if r.ReletMedianEURPerM2 != 20.3 {
		t.Errorf("zone5/p2 derived relet = %.1f, want 20.3 (zone ratio applied)", r.ReletMedianEURPerM2)
	}

	// Relet is never below all-tenancies where the ratio ≥ 1 (these zones rose).
	for _, r := range out {
		if r.ReletMedianEURPerM2 > 0 && r.ReletMedianEURPerM2 < r.MedianEURPerM2 {
			t.Errorf("zone%s/p%d relet %.1f < all %.1f (unexpected for a rising zone)", r.Zone, r.Pieces, r.ReletMedianEURPerM2, r.MedianEURPerM2)
		}
	}
}

// TestParseRents_ReletPreferredWhenObservedBelowAll documents that a genuinely
// falling zone (observed relet < all) is preserved, not clamped — the relet
// signal reflects the market, not an assumption.
func TestParseRents_ReletBelowAllPreserved(t *testing.T) {
	t.Parallel()
	rows := []string{
		reletCSVHeader,
		reletRow("L0600.4.08", "", "", "14,9"),
		reletRow("L0600.4.08", "", olrReletLabel, "11,5"), // relet below all (real, e.g. small sample)
	}
	out, err := parseRents(strings.Join(rows, "\n") + "\n")
	if err != nil {
		t.Fatalf("parseRents: %v", err)
	}
	r, ok := findRow(out, "8", 0)
	if !ok {
		t.Fatal("zone 8 cell missing")
	}
	if r.ReletMedianEURPerM2 != 11.5 {
		t.Errorf("relet = %.1f, want 11.5 (observed, not clamped up to all)", r.ReletMedianEURPerM2)
	}
}

// TestEmbeddedRelet_Zone5AndTendency validates the shipped artifact: Saint-Denis
// (L7502 zone 5) carries the measured relet median (~19.5 €/m²/month), and
// across every agglo the relet level is a positive uplift in the large majority
// of cells (the "moins de 1 an" market generally re-lets above the
// all-tenancies median).
func TestEmbeddedRelet_Zone5AndTendency(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Saint-Denis (93066) → L7502 zone 5, all-sizes aggregate (pieces 0).
	_, cell, ok := idx.Lookup("93066", 0)
	if !ok {
		t.Fatal("Saint-Denis (93066) not found in the embedded OLL index")
	}
	if cell.ReletMedianEURPerM2 < 19.0 || cell.ReletMedianEURPerM2 > 20.0 {
		t.Errorf("Saint-Denis zone-5 relet median = %.1f, want ~19.5 (DRIHL/OLL 2024 relet)", cell.ReletMedianEURPerM2)
	}
	if cell.ReletMedianEURPerM2 <= cell.MedianEURPerM2 {
		t.Errorf("Saint-Denis relet %.1f not above all-tenancies %.1f", cell.ReletMedianEURPerM2, cell.MedianEURPerM2)
	}

	// Tendency: relet ≥ all in the large majority of cells that carry a relet
	// level (a few zones genuinely re-let below — small samples, softening
	// markets — so this is a strong tendency, not a hard invariant).
	var withRelet, atOrAbove int
	for _, r := range idx.rents {
		if r.ReletMedianEURPerM2 <= 0 {
			continue
		}
		withRelet++
		if r.ReletMedianEURPerM2 >= r.MedianEURPerM2 {
			atOrAbove++
		}
	}
	if withRelet == 0 {
		t.Fatal("no cells carry a relet median — the relet ingest is not populated")
	}
	if ratio := float64(atOrAbove) / float64(withRelet); ratio < 0.9 {
		t.Errorf("only %.0f%% of relet cells are ≥ all-tenancies, want ≥ 90%%", ratio*100)
	}
}
