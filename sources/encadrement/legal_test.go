package encadrement

import (
	"os"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestMain pins the clock a Listing without AsOf reads to the day the shipped
// grilles were last checked, so the suite does not change verdict on its own
// when the art. 140 experiment ends: a test that needs another date sets
// Listing.AsOf.
func TestMain(m *testing.M) {
	clock = func() time.Time { return time.Date(2026, time.October, 21, 12, 0, 0, 0, time.UTC) }
	os.Exit(m.Run())
}

// The caps rest on the art. 140 experiment, whose eight years end on
// 24 November 2026. The Source used to quote them for any date, so an auction
// heard in 2027 would have been told a cap that is no longer law.
func TestQuery_HonoursTheExperimentEnd(t *testing.T) {
	t.Parallel()
	paris := gazetteer.Listing{INSEE: "75101", IRIS: "751010201", Rooms: intp(1), BuildYear: intp(2000)}
	at := func(l gazetteer.Listing, day time.Time) *Result {
		l.AsOf = day
		return query(t, l)
	}

	// Last day: still law, the arrêté's own last day too.
	if r := at(paris, civilDate(2026, time.November, 24)); r.Regulation != RegulationCapped || r.LoyerRefMajEURPerM2HC != 37.8 || r.Confidence != ConfidenceMedium {
		t.Errorf("24 Nov: %q %.2f %q, want capped 37.8 medium", r.Regulation, r.LoyerRefMajEURPerM2HC, r.Confidence)
	}
	// Late on 24 November in Paris time is still 24 November.
	paris2330 := time.Date(2026, time.November, 24, 23, 30, 0, 0, time.FixedZone("CET", 3600))
	if r := at(paris, paris2330); r.Regulation != RegulationCapped {
		t.Errorf("24 Nov 23:30 CET: %q, want capped", r.Regulation)
	}

	// The day after: no cap, said so.
	for _, l := range []gazetteer.Listing{
		paris,
		{INSEE: "69383", Rooms: intp(2)},
		{INSEE: "93048", Rooms: intp(2)},
		{INSEE: "33063", Rooms: intp(2)}, // a territory without a shipped grille lapses too
	} {
		r := at(l, civilDate(2026, time.November, 25))
		if r.Regulation != RegulationLapsed || !r.IsEmpty() || r.Confidence != ConfidenceNone {
			t.Errorf("%s 25 Nov: Regulation %q IsEmpty %v Confidence %q, want lapsed, empty, none", l.INSEE, r.Regulation, r.IsEmpty(), r.Confidence)
		}
		if r.LoyerRefMajEURPerM2HC != 0 || r.LoyerRefEURPerM2HC != 0 || r.LoyerRefMajMeubleEURPerM2HC != 0 || r.LoyerRefMeubleEURPerM2HC != 0 {
			t.Errorf("%s 25 Nov: a cap figure survived the lapse: %+v", l.INSEE, r)
		}
		if r.Evidence.Note == "" {
			t.Errorf("%s 25 Nov: no Evidence.Note", l.INSEE)
		}
		if _, ok := r.RentCap(); ok {
			t.Errorf("%s 25 Nov: RentCap ok", l.INSEE)
		}
	}

	// A commune in no territory is unchanged by the date.
	if r := at(gazetteer.Listing{INSEE: "31555"}, civilDate(2027, time.January, 1)); r.Regulation != "" || !r.IsEmpty() {
		t.Errorf("Toulouse 2027: Regulation %q, want empty", r.Regulation)
	}

	// Without AsOf the clock decides (pinned to 2026-10-21 by TestMain).
	if r := query(t, paris); r.Regulation != RegulationCapped {
		t.Errorf("no AsOf: %q, want capped", r.Regulation)
	}
}

// Each grille has its own period. Before it the previous arrêté applied, which
// is not shipped: the cap is kept at low confidence with a Note. Past it,
// which an extension of the experiment would make reachable, the shipped
// cells are no longer law and the figure is withdrawn.
func TestDated_GrillePeriod(t *testing.T) {
	t.Parallel()
	paris := gazetteer.Listing{INSEE: "75101", IRIS: "751010201", Rooms: intp(1), BuildYear: intp(2000)}

	before := paris
	before.AsOf = civilDate(2026, time.March, 1)
	if r := query(t, before); r.LoyerRefMajEURPerM2HC != 37.8 || r.Confidence != ConfidenceLow || r.Evidence.Note == "" {
		t.Errorf("Paris before 1 Jul 2026: maj %.2f %q note %q, want 37.8 low with a note", r.LoyerRefMajEURPerM2HC, r.Confidence, r.Evidence.Note)
	}
	// Lyon's grille is in force since 1 November 2025: March 2026 is inside.
	lyon := gazetteer.Listing{INSEE: "69381", IRIS: "693810101", Rooms: intp(2), AsOf: civilDate(2026, time.March, 1)}
	if r := query(t, lyon); r.Confidence != ConfidenceMedium || r.Evidence.Note != "" {
		t.Errorf("Lyon March 2026: %q note %q, want medium, no note", r.Confidence, r.Evidence.Note)
	}

	// Simulate an extension to 2028: the Paris arrêté still ends on
	// 24 November 2026, so 2027 reads regulated, figure unknown.
	capped := query(t, paris)
	r := dated(capped, ZoneSourceParis, civilDate(2027, time.January, 15), civilDate(2028, time.November, 24))
	if r.Regulation != RegulationNotCovered || r.LoyerRefMajEURPerM2HC != 0 || r.LoyerRefMajMeubleEURPerM2HC != 0 || r.IsEmpty() || r.Evidence.Note == "" {
		t.Errorf("Paris 2027 under an extension: %+v, want not covered, no figure, a note", r)
	}
	// Lyon's arrêté states no end: under the same extension it still reads.
	lr := query(t, gazetteer.Listing{INSEE: "69381", Rooms: intp(2)})
	if got := dated(lr, ZoneSourceLyonVilleurbanne, civilDate(2027, time.January, 15), civilDate(2028, time.November, 24)); got.Regulation != RegulationCapped {
		t.Errorf("Lyon 2027 under an extension: %q, want capped", got.Regulation)
	}
}

// The legal constants must agree with each other: no grille may be in force
// past the experiment it rests on, and every shipped zone has a period.
func TestGridValidity_Consistent(t *testing.T) {
	t.Parallel()
	for _, zs := range []string{ZoneSourceParis, ZoneSourcePlaineCommune, ZoneSourceEstEnsemble, ZoneSourceLyonVilleurbanne} {
		v, ok := gridValidity[zs]
		if !ok {
			t.Errorf("%s: no grille period", zs)
			continue
		}
		if v.from.IsZero() || v.arrete == "" {
			t.Errorf("%s: incomplete period %+v", zs, v)
		}
		if !v.until.IsZero() && (v.until.Before(v.from) || v.until.After(elanLastDay)) {
			t.Errorf("%s: until %s outside [%s, %s]", zs, v.until.Format(time.DateOnly), v.from.Format(time.DateOnly), elanLastDay.Format(time.DateOnly))
		}
	}
}
