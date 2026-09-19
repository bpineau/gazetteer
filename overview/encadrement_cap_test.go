package overview

import (
	"testing"

	"github.com/bpineau/gazetteer/sources/encadrement"
)

func TestRepresentativeT2Majore(t *testing.T) {
	t.Parallel()

	idx, err := encadrement.Load("")
	if err != nil {
		t.Fatalf("encadrement.Load: %v", err)
	}

	// Paris arrondissement 19e (75119) should return a positive cap.
	cap, ok := RepresentativeT2Majore(idx, "75119")
	if !ok {
		t.Fatal("RepresentativeT2Majore(75119) = _, false; want true (Paris 19e is encadré)")
	}
	if cap <= 0 {
		t.Fatalf("RepresentativeT2Majore(75119) = %v, want > 0", cap)
	}
	t.Logf("Paris 75119 T2 majoré cap = %.2f €/m²/mois HC", cap)

	// Plaine Commune commune (Saint-Denis 93066) should return a positive cap.
	capPC, okPC := RepresentativeT2Majore(idx, "93066")
	if !okPC {
		t.Fatal("RepresentativeT2Majore(93066) = _, false; want true (Saint-Denis is Plaine Commune)")
	}
	if capPC <= 0 {
		t.Fatalf("RepresentativeT2Majore(93066) = %v, want > 0", capPC)
	}
	t.Logf("Plaine Commune 93066 T2 majoré cap = %.2f €/m²/mois HC", capPC)

	// Est Ensemble commune (Montreuil 93048) should return a positive cap.
	capEE, okEE := RepresentativeT2Majore(idx, "93048")
	if !okEE {
		t.Fatal("RepresentativeT2Majore(93048) = _, false; want true (Montreuil is Est Ensemble)")
	}
	if capEE <= 0 {
		t.Fatalf("RepresentativeT2Majore(93048) = %v, want > 0", capEE)
	}
	t.Logf("Est Ensemble 93048 T2 majoré cap = %.2f €/m²/mois HC", capEE)

	// Non-encadré commune (Provins 77284) should return ok=false.
	_, okNone := RepresentativeT2Majore(idx, "77284")
	if okNone {
		t.Fatal("RepresentativeT2Majore(77284) = _, true; want false (Provins is not encadré)")
	}
}

// TestRepresentativeT2Majore_Lyon is the regression for the Métropole de Lyon
// perimeter, which the commune screen used to miss entirely: the nine Lyon
// arrondissements and Villeurbanne came back ok=false, so CommuneOverview
// marked them Encadree=false and EffectiveRentEURM2HC left their market rent
// uncapped — the one place a screen must not be optimistic.
func TestRepresentativeT2Majore_Lyon(t *testing.T) {
	t.Parallel()

	idx, err := encadrement.Load("")
	if err != nil {
		t.Fatalf("encadrement.Load: %v", err)
	}
	for _, insee := range []string{"69381", "69383", "69389", "69266"} {
		cap, ok := RepresentativeT2Majore(idx, insee)
		if !ok {
			t.Errorf("RepresentativeT2Majore(%s) = _, false; want true (inside the Lyon perimeter)", insee)
			continue
		}
		// The published T2 majoré across the Métropole sits in a narrow band;
		// anything outside it means the lookup grabbed the wrong cells.
		if cap < 10 || cap > 30 {
			t.Errorf("RepresentativeT2Majore(%s) = %.2f, want 10..30 €/m²/mois HC", insee, cap)
		}
	}
	// The Lyon parent commune carries no grille of its own (dvfagg keys the
	// arrondissements), and must not borrow one.
	if _, ok := RepresentativeT2Majore(idx, "69123"); ok {
		t.Error("RepresentativeT2Majore(69123) = _, true; want false (parent commune, no grille)")
	}
}
