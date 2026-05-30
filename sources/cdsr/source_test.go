package cdsr

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestLoad smokes the embedded snapshot.
func TestLoad(t *testing.T) {
	t.Parallel()
	cat, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := len(cat.Copros); got < 15 {
		t.Errorf("catalog size = %d, want ≥ 15", got)
	}
	for i, c := range cat.Copros {
		if c.Lat == 0 && c.Lon == 0 {
			t.Fatalf("copro %d (%s) has no coordinates", i, c.Commune)
		}
	}
}

// TestQuery_NearCopro resolves a listing sitting on a known CDSR copro
// (Résidence La Bruyère, Bondy).
func TestQuery_NearCopro(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{Lat: new(48.907676), Lon: new(2.491884)}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.IsEmpty() {
		t.Fatalf("IsEmpty() = true, want a hit on the Bondy copro")
	}
	if res.NearestM > 50 {
		t.Errorf("NearestM = %d, want ≤ 50 (listing is on the copro)", res.NearestM)
	}
	if res.Within500m < 1 || res.Within3km < 1 {
		t.Errorf("Within500m=%d Within3km=%d, want ≥ 1 each", res.Within500m, res.Within3km)
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
	if len(res.Nearest) == 0 || res.Nearest[0].Commune != "BONDY" {
		t.Errorf("Nearest[0] commune = %v, want BONDY", res.Nearest)
	}
	if res.Nearest[0].Lots <= 0 || res.Nearest[0].LabelYear == 0 {
		t.Errorf("Nearest[0] lots/year = %d/%d, want populated", res.Nearest[0].Lots, res.Nearest[0].LabelYear)
	}
}

// TestQuery_FarAway returns an empty result well outside Île-de-France.
func TestQuery_FarAway(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{Lat: new(43.2965), Lon: new(5.3698)} // Marseille
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !res.IsEmpty() {
		t.Errorf("IsEmpty() = false, want true for Marseille")
	}
	if len(res.Nearest) != 0 || res.NearestM != 0 {
		t.Errorf("got Nearest=%v NearestM=%d, want none", res.Nearest, res.NearestM)
	}
}

// TestQuery_MissingCoords skips without coordinates.
func TestQuery_MissingCoords(t *testing.T) {
	t.Parallel()
	cases := []gazetteer.Listing{
		{INSEE: "93008"},               // no lat/lon
		{Lat: new(0.0), Lon: new(0.0)}, // 0,0 sentinel
	}
	for _, l := range cases {
		if _, err := Query(context.Background(), Options{}, l); !errors.Is(err, gazetteer.ErrInsufficientInputs) {
			t.Errorf("Query(%+v) err = %v, want ErrInsufficientInputs", l, err)
		}
	}
}

// TestQuery_Capped ensures Nearest never exceeds maxNearestItems even when many
// copros fall within range (a stub catalog of clustered points).
func TestQuery_Capped(t *testing.T) {
	t.Parallel()
	var copros []Copro
	for i := range maxNearestItems + 3 {
		copros = append(copros, Copro{Commune: "X", Lat: 48.9, Lon: 2.4 + float64(i)*0.0001, Lots: 10, LabelYear: 2018})
	}
	res, err := Query(context.Background(), Options{Catalog: &Catalog{Copros: copros}}, gazetteer.Listing{Lat: new(48.9), Lon: new(2.4)})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(res.Nearest) != maxNearestItems {
		t.Errorf("len(Nearest) = %d, want %d (capped)", len(res.Nearest), maxNearestItems)
	}
	if res.Within3km != maxNearestItems+3 {
		t.Errorf("Within3km = %d, want %d (count is not capped)", res.Within3km, maxNearestItems+3)
	}
	// All stub points sit within ~60 m, so every one is counted within 500 m.
	if res.Within500m != maxNearestItems+3 {
		t.Errorf("Within500m = %d, want %d", res.Within500m, maxNearestItems+3)
	}
	// Nearest must be sorted by ascending distance.
	for i := 1; i < len(res.Nearest); i++ {
		if res.Nearest[i].DistanceM < res.Nearest[i-1].DistanceM {
			t.Errorf("Nearest not ascending: [%d]=%dm < [%d]=%dm", i, res.Nearest[i].DistanceM, i-1, res.Nearest[i-1].DistanceM)
		}
	}
}
