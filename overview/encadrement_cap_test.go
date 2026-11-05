package overview

import (
	"testing"
	"time"

	"github.com/bpineau/gazetteer/sources/encadrement"
)

// grilleDay is a date every shipped grille is in force on, so the cap tests
// do not change verdict on their own when the art. 140 experiment ends.
var grilleDay = time.Date(2026, time.October, 21, 0, 0, 0, 0, time.UTC)

func TestRepresentativeT2Majore(t *testing.T) {
	t.Parallel()

	idx, err := encadrement.Load("")
	if err != nil {
		t.Fatalf("encadrement.Load: %v", err)
	}

	// Paris arrondissement 19e (75119) should return a positive cap.
	cap, ok := RepresentativeT2Majore(idx, "75119", grilleDay)
	if !ok {
		t.Fatal("RepresentativeT2Majore(75119) = _, false; want true (Paris 19e is encadré)")
	}
	if cap <= 0 {
		t.Fatalf("RepresentativeT2Majore(75119) = %v, want > 0", cap)
	}
	t.Logf("Paris 75119 T2 majoré cap = %.2f €/m²/mois HC", cap)

	// Plaine Commune commune (Saint-Denis 93066) should return a positive cap.
	capPC, okPC := RepresentativeT2Majore(idx, "93066", grilleDay)
	if !okPC {
		t.Fatal("RepresentativeT2Majore(93066) = _, false; want true (Saint-Denis is Plaine Commune)")
	}
	if capPC <= 0 {
		t.Fatalf("RepresentativeT2Majore(93066) = %v, want > 0", capPC)
	}
	t.Logf("Plaine Commune 93066 T2 majoré cap = %.2f €/m²/mois HC", capPC)

	// Est Ensemble commune (Montreuil 93048) should return a positive cap.
	capEE, okEE := RepresentativeT2Majore(idx, "93048", grilleDay)
	if !okEE {
		t.Fatal("RepresentativeT2Majore(93048) = _, false; want true (Montreuil is Est Ensemble)")
	}
	if capEE <= 0 {
		t.Fatalf("RepresentativeT2Majore(93048) = %v, want > 0", capEE)
	}
	t.Logf("Est Ensemble 93048 T2 majoré cap = %.2f €/m²/mois HC", capEE)

	// Non-encadré commune (Provins 77284) should return ok=false.
	_, okNone := RepresentativeT2Majore(idx, "77284", grilleDay)
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
		cap, ok := RepresentativeT2Majore(idx, insee, grilleDay)
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
	if _, ok := RepresentativeT2Majore(idx, "69123", grilleDay); ok {
		t.Error("RepresentativeT2Majore(69123) = _, true; want false (parent commune, no grille)")
	}
}

// A decreed rent-cap territory whose grille is not embedded (Bordeaux) used to
// be screened Encadree=false, as if its rents were free. It is now Encadree
// with EncadrementNotCovered and no cap figure; Toulouse, in no decreed
// territory, stays unregulated.
func TestBuild_DecreedTerritoryWithoutGrille(t *testing.T) {
	t.Parallel()
	rows, err := Build(Options{Depts: []string{"31", "33"}, AsOf: grilleDay})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	found := map[string]CommuneOverview{}
	for _, r := range rows {
		found[r.INSEE] = r
	}
	bdx, ok := found["33063"]
	if !ok {
		t.Fatal("no Bordeaux row")
	}
	if !bdx.Encadree || !bdx.EncadrementNotCovered || bdx.RentCapEURM2HC != nil {
		t.Errorf("Bordeaux: Encadree %v NotCovered %v cap %v, want true true nil", bdx.Encadree, bdx.EncadrementNotCovered, bdx.RentCapEURM2HC)
	}
	tls, ok := found["31555"]
	if !ok {
		t.Fatal("no Toulouse row")
	}
	if tls.Encadree || tls.EncadrementNotCovered {
		t.Errorf("Toulouse: Encadree %v NotCovered %v, want false false", tls.Encadree, tls.EncadrementNotCovered)
	}
}

// TestBuild_EncadrementFollowsItsValidity pins that the screen applies the
// encadrement Source's date rule: the cap is plain up to the published end
// of the art. 140 experiment (24 November 2026), hedged under the assumed
// extension after it, and gone (with no not-covered flag) the day after the
// assumed end.
func TestBuild_EncadrementFollowsItsValidity(t *testing.T) {
	t.Parallel()
	last := time.Date(2026, time.November, 24, 0, 0, 0, 0, time.UTC)
	rowOf := func(day time.Time, insee string) CommuneOverview {
		t.Helper()
		rows, err := Build(Options{Depts: []string{"75", "33"}, AsOf: day})
		if err != nil {
			t.Fatalf("Build: %v", err)
		}
		for _, r := range rows {
			if r.INSEE == insee {
				return r
			}
		}
		t.Fatalf("no %s row", insee)
		return CommuneOverview{}
	}
	if r := rowOf(last, "75101"); !r.Encadree || r.RentCapEURM2HC == nil || r.EncadrementLapsed || r.RentCapHedged {
		t.Errorf("75101 on %s: Encadree %v cap %v lapsed %v hedged %v, want a plain cap in force", last.Format(time.DateOnly), r.Encadree, r.RentCapEURM2HC, r.EncadrementLapsed, r.RentCapHedged)
	}
	assumed := last.AddDate(0, 0, 1)
	if r := rowOf(assumed, "75101"); !r.Encadree || r.RentCapEURM2HC == nil || !r.RentCapHedged {
		t.Errorf("75101 on %s: Encadree %v cap %v hedged %v, want a hedged cap", assumed.Format(time.DateOnly), r.Encadree, r.RentCapEURM2HC, r.RentCapHedged)
	}
	after := time.Date(2028, time.November, 25, 0, 0, 0, 0, time.UTC)
	for _, insee := range []string{"75101", "33063"} {
		r := rowOf(after, insee)
		if r.Encadree || r.RentCapEURM2HC != nil || r.EncadrementNotCovered || !r.EncadrementLapsed {
			t.Errorf("%s on %s: Encadree %v cap %v notCovered %v lapsed %v, want lapsed with no cap",
				insee, after.Format(time.DateOnly), r.Encadree, r.RentCapEURM2HC, r.EncadrementNotCovered, r.EncadrementLapsed)
		}
	}
}
