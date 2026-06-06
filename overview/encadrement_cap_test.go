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
