package dvf

import (
	"testing"
	"time"
)

// cultureRow is one row of a house sale as geo-dvf publishes it: the SAME
// local repeated once per nature de culture of its parcel (here "sols" and
// "terrains d'agrément"), with the mutation's whole price on each row.
func cultureRow(id, parcelle string, surface, valeur float64) Mutation {
	m := mkLot(id, "Vente", "Maison", "2024-06-01", surface, valeur)
	m.IDParcelle = parcelle
	pieces := 5
	m.NombrePiecesPrinc = &pieces
	return m
}

// A house on a parcel carrying two natures de culture is ONE built local
// published on two rows. The single-built-local rule counted rows, so it
// read every such house as a two-house bundle and dropped it: on four live
// sections of a Seine-et-Marne commune (77284, sections 000AB..000AE) it
// kept 14 of the 32 house sales, and the survivors were the houses without
// a garden. The rule now counts DISTINCT locals and keeps one row of each,
// so the house enters the cohort once, at its own price over its own surface.
func TestFilterMutations_CultureRowsAreOneLocal(t *testing.T) {
	window := Window{From: time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)}
	in := []Mutation{
		cultureRow("garden", "77284000AC0726", 133, 400_000),
		cultureRow("garden", "77284000AC0726", 133, 400_000),
		// Two real houses sold together stay a bundle and stay out.
		cultureRow("pair", "77284000AC0800", 90, 500_000),
		cultureRow("pair", "77284000AC0801", 110, 500_000),
	}
	got := FilterMutations(in, "Maison", window)
	if len(got) != 1 || got[0].IDMutation != "garden" {
		t.Fatalf("FilterMutations = %+v, want the garden house once", got)
	}
	if _, p50, _ := PerM2Quartiles(got); p50 < 3007 || p50 > 3008 {
		t.Errorf("median = %.0f EUR/m2, want 400 000 / 133 = 3 008", p50)
	}
}

// Two distinct flats of one copro parcel, told apart by their lot number,
// are two locals even at the same surface and room count.
func TestFilterMutations_LotsTellFlatsApart(t *testing.T) {
	window := Window{From: time.Date(2021, 1, 1, 0, 0, 0, 0, time.UTC)}
	a := mkLot("twins", "Vente", "Appartement", "2024-06-01", 40, 800_000)
	b := a
	a.IDParcelle, b.IDParcelle = "75107000AD0049", "75107000AD0049"
	a.Lot1Numero, b.Lot1Numero = "12", "13"
	if got := FilterMutations([]Mutation{a, b}, "Appartement", window); len(got) != 0 {
		t.Fatalf("FilterMutations kept %d rows of a two-flat bundle, want 0", len(got))
	}
}
