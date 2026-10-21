package encadrement

import (
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// Plaine Commune and Est Ensemble publish a separate MAISON grille, and in
// zone 311 it sits about 16 % above the apartment one. A house used to read
// the apartment cell regardless (cellFilter dropped every maison cell), so its
// legal ceiling came out too low. A house now reads the maison cells where the
// zone publishes them, and the shared grille where it does not (Paris and
// Lyon set one cap for every dwelling type).
func TestQuery_HouseReadsTheMaisonGrille(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cell := func(maison bool) float64 {
		for _, e := range idx.LookupEPTZone(ZoneSourcePlaineCommune, "311") {
			if e.Maison == maison && !e.Meuble && e.Piece == 3 && e.Epoque == "avant 1946" {
				return e.LoyerRefMaxEURPerM2HC
			}
		}
		t.Fatalf("no zone 311 3-room avant-1946 cell (maison=%v)", maison)
		return 0
	}
	wantHouse, wantFlat := cell(true), cell(false)
	if wantHouse == wantFlat {
		t.Fatalf("fixture: zone 311 maison and apartment cells agree (%.2f), pick another", wantHouse)
	}
	pt := gazetteer.Listing{INSEE: "93066", Lat: new(48.9190), Lon: new(2.3540), Rooms: intp(3), BuildYear: intp(1930)}

	house := pt
	house.PropertyType = gazetteer.PropertyHouse
	if r := queryAs(t, house); r.LoyerRefMajEURPerM2HC != wantHouse || r.Evidence.ZoneID != "311" {
		t.Errorf("house: maj %.2f zone %q, want the maison cell %.2f in 311", r.LoyerRefMajEURPerM2HC, r.Evidence.ZoneID, wantHouse)
	}
	if r := query(t, pt); r.LoyerRefMajEURPerM2HC != wantFlat {
		t.Errorf("apartment: maj %.2f, want the apartment cell %.2f", r.LoyerRefMajEURPerM2HC, wantFlat)
	}

	// Paris publishes one grille for every dwelling type: a house reads it.
	parisHouse := gazetteer.Listing{INSEE: "75116", Rooms: intp(4), BuildYear: intp(1930), PropertyType: gazetteer.PropertyHouse}
	parisFlat := parisHouse
	parisFlat.PropertyType = gazetteer.PropertyApartment
	if h, f := queryAs(t, parisHouse), queryAs(t, parisFlat); h.IsEmpty() || h.LoyerRefMajEURPerM2HC != f.LoyerRefMajEURPerM2HC {
		t.Errorf("Paris house: maj %.2f, want the shared grille %.2f", h.LoyerRefMajEURPerM2HC, f.LoyerRefMajEURPerM2HC)
	}
}

// queryAs is query without forcing the property type to apartment.
func queryAs(t *testing.T, l gazetteer.Listing) *Result {
	t.Helper()
	res, err := Query(t.Context(), Options{}, l)
	if err != nil || res == nil {
		t.Fatalf("Query(%+v): %v", l, err)
	}
	return res
}
