package encadrement

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestMain pins the clock a Listing without AsOf reads to the day the shipped
// grilles were last checked, so the suite does not change verdict on its own
// when the art. 140 experiment ends: a test that needs another date sets
// Listing.AsOf.
func TestMain(m *testing.M) {
	clock = func() time.Time { return time.Date(2026, time.November, 26, 12, 0, 0, 0, time.UTC) }
	os.Exit(m.Run())
}

func TestQuery_HonoursTheExperimentEnd(t *testing.T) {
	t.Parallel()
	paris := gazetteer.Listing{INSEE: "75101", IRIS: "751010201", Rooms: intp(1), BuildYear: intp(2000)}
	at := func(l gazetteer.Listing, day time.Time) *Result {
		l.AsOf = day
		return query(t, l)
	}

	// The Paris rate continues across the former sunset.
	if r := at(paris, civilDate(2026, time.November, 24)); r.Regulation != RegulationCapped || r.LoyerRefMajEURPerM2HC != 37.8 || r.Confidence != ConfidenceMedium {
		t.Errorf("24 Nov: %q %.2f %q, want capped 37.8 medium", r.Regulation, r.LoyerRefMajEURPerM2HC, r.Confidence)
	}
	// Late on 24 November in Paris time is still 24 November.
	paris2330 := time.Date(2026, time.November, 24, 23, 30, 0, 0, time.FixedZone("CET", 3600))
	if r := at(paris, paris2330); r.Regulation != RegulationCapped {
		t.Errorf("24 Nov 23:30 CET: %q, want capped", r.Regulation)
	}

	for _, insee := range []string{"75101", "93048", "93066", "69383"} {
		l := gazetteer.Listing{INSEE: insee, Rooms: intp(2)}
		a := at(l, civilDate(2026, time.November, 24))
		b := at(l, civilDate(2026, time.November, 25))
		if b.Regulation != RegulationCapped || b.LoyerRefMajEURPerM2HC <= 0 || a.LoyerRefMajEURPerM2HC != b.LoyerRefMajEURPerM2HC || a.Confidence != b.Confidence || a.Evidence.Note != b.Evidence.Note {
			t.Errorf("%s: reading changed across the former sunset: before %+v, after %+v", insee, a, b)
		}
	}

	for _, l := range []gazetteer.Listing{
		paris,
		{INSEE: "69383", Rooms: intp(2)},
		{INSEE: "93048", Rooms: intp(2)},
		{INSEE: "33063", Rooms: intp(2)}, // a territory without a shipped grille lapses too
	} {
		r := at(l, civilDate(2028, time.November, 25))
		if r.Regulation != RegulationLapsed || !r.IsEmpty() || r.Confidence != ConfidenceNone {
			t.Errorf("%s 25 Nov 2028: Regulation %q IsEmpty %v Confidence %q, want lapsed, empty, none", l.INSEE, r.Regulation, r.IsEmpty(), r.Confidence)
		}
		if r.LoyerRefMajEURPerM2HC != 0 || r.LoyerRefEURPerM2HC != 0 || r.LoyerRefMajMeubleEURPerM2HC != 0 || r.LoyerRefMeubleEURPerM2HC != 0 {
			t.Errorf("%s 25 Nov 2028: a cap figure survived the lapse: %+v", l.INSEE, r)
		}
		if r.Evidence.Note == "" {
			t.Errorf("%s 25 Nov 2028: no Evidence.Note", l.INSEE)
		}
		if _, ok := r.RentCap(); ok {
			t.Errorf("%s 25 Nov 2028: RentCap ok", l.INSEE)
		}
	}

	// A commune in no territory is unchanged by the date.
	if r := at(gazetteer.Listing{INSEE: "31555"}, civilDate(2027, time.January, 1)); r.Regulation != "" || !r.IsEmpty() {
		t.Errorf("Toulouse 2027: Regulation %q, want empty", r.Regulation)
	}

	// Without AsOf the clock decides (pinned to 2026-11-26 by TestMain).
	if r := query(t, paris); r.Regulation != RegulationCapped {
		t.Errorf("no AsOf: %q, want capped", r.Regulation)
	}
}

// Each grille has its own period. Before it the previous arrêté applied, which
// is not shipped: the cap is kept at low confidence with a Note. Past it,
// which the extension makes reachable, the successor's cells are not shipped
// and the shipped ones are carried over at low confidence.
func TestDated_GrillePeriod(t *testing.T) {
	t.Parallel()
	paris := gazetteer.Listing{INSEE: "75101", IRIS: "751010201", Rooms: intp(1), BuildYear: intp(2000)}

	before := paris
	before.AsOf = civilDate(2026, time.March, 1)
	if r := query(t, before); r.LoyerRefMajEURPerM2HC != 37.8 || r.Confidence != ConfidenceLow || r.Evidence.Note == "" {
		t.Errorf("Paris before 1 Jul 2026: maj %.2f %q note %q, want 37.8 low with a note", r.LoyerRefMajEURPerM2HC, r.Confidence, r.Evidence.Note)
	}
	lyon := gazetteer.Listing{INSEE: "69381", IRIS: "693810101", Rooms: intp(2)}
	for _, tc := range []struct {
		day     time.Time
		carried bool
	}{
		{civilDate(2026, time.November, 1), false},
		{civilDate(2027, time.October, 31), false},
		{civilDate(2027, time.November, 1), true},
	} {
		lyon.AsOf = tc.day
		r := query(t, lyon)
		if r.Regulation != RegulationCapped || strings.Contains(r.Evidence.Note, "carried over") != tc.carried || (r.Confidence == ConfidenceLow) != tc.carried || CapHedgedOn(ZoneSourceLyonVilleurbanne, tc.day) != tc.carried {
			t.Errorf("Lyon on %s: %+v, want carried over %v", tc.day.Format(time.DateOnly), r, tc.carried)
		}
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

// TestRegulationOn pins the batch date rule on both sides of the shipped
// boundaries: the Paris grille's start, and the experiment's last day.
func TestRegulationOn(t *testing.T) {
	t.Parallel()
	cases := []struct {
		zoneSource string
		day        time.Time
		want       string
	}{
		{ZoneSourceParis, civilDate(2026, time.June, 30), RegulationCapped}, // before the grille: nearest reading
		{ZoneSourceParis, civilDate(2026, time.November, 24), RegulationCapped},
		{ZoneSourceParis, civilDate(2026, time.November, 25), RegulationCapped},
		{ZoneSourceParis, civilDate(2028, time.November, 24), RegulationCapped},
		{ZoneSourceParis, civilDate(2028, time.November, 25), RegulationLapsed},
		{ZoneSourceLyonVilleurbanne, civilDate(2028, time.November, 25), RegulationLapsed},
		{"", civilDate(2026, time.November, 25), RegulationNotCovered},
		{"", civilDate(2028, time.November, 25), RegulationLapsed},
		{ZoneSourceParis, time.Time{}, RegulationCapped}, // zero day = the pinned clock
	}
	for _, c := range cases {
		if got := RegulationOn(c.zoneSource, c.day); got != c.want {
			t.Errorf("RegulationOn(%q, %s) = %q, want %q", c.zoneSource, c.day.Format(time.DateOnly), got, c.want)
		}
	}
}
