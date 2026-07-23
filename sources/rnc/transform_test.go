package rnc

import (
	"bytes"
	"context"
	"io"
	"os"
	"sort"
	"testing"
)

// fixtureRawSet serves a single named file from testdata, implementing
// dataset.RawSet for the transform under test.
type fixtureRawSet struct{ path string }

func (f fixtureRawSet) Open(string) (io.ReadCloser, error) { return os.Open(f.path) }

func TestTransform_Golden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := transform(context.Background(), fixtureRawSet{"testdata/rnc_sample.csv"}, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}
	if err := validate(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validate: %v", err)
	}
	idx, err := parseIndexStream(bytes.NewReader(buf.Bytes()), nil)
	if err != nil {
		t.Fatalf("parseIndexStream: %v", err)
	}
	if idx.Count() != 4 {
		t.Fatalf("Count = %d, want 4", idx.Count())
	}

	by := map[string]Entry{}
	for _, e := range idx.Copros {
		by[e.Immatriculation] = e
	}

	// Row 1: clean, professional syndic, small — no amber signal.
	a1 := by["AA0000001"]
	if got := amberSignals(ptrEntry(a1)); len(got) != 0 {
		t.Errorf("AA0000001 signals = %v, want none", got)
	}
	// Voie normalized (street-type marker stripped, number dropped).
	if a1.VoieNorm == "" || a1.VoieNorm == "20 r de gramont" {
		t.Errorf("AA0000001 VoieNorm = %q, want normalized", a1.VoieNorm)
	}
	// INSEE is the COG code from code_officiel_arrondissement_commune (75102),
	// NOT the postal code in the mislabeled code_officiel_commune (75002).
	if a1.INSEE != "75102" {
		t.Errorf("AA0000001 INSEE = %q, want 75102 (COG, not the 75002 postal code)", a1.INSEE)
	}
	// Newly-read columns: cadastre, parking lots, last-mandate end date.
	if !eq(a1.Parcelles, []string{"75102000AB0012"}) {
		t.Errorf("AA0000001 Parcelles = %v, want [75102000AB0012]", a1.Parcelles)
	}
	if a1.LotsStationnement != 5 {
		t.Errorf("AA0000001 LotsStationnement = %d, want 5", a1.LotsStationnement)
	}
	if a1.MandatFin != "2027-06-30" {
		t.Errorf("AA0000001 MandatFin = %q, want 2027-06-30", a1.MandatFin)
	}

	// A non-PLM commune: COG 91286, not the 91350 postal code.
	if got := by["CC0000003"].INSEE; got != "91286" {
		t.Errorf("CC0000003 INSEE = %q, want 91286 (COG, not the 91350 postal code)", got)
	}

	// Row 2: governance vacuum + undeclared syndic on a LARGE copro (80 lots),
	// so the size-gated syndic signal fires; two cadastral parcelles read.
	b2 := by["BB0000002"]
	if !eq(b2.Parcelles, []string{"75111000CD0034", "75111000CD0035"}) {
		t.Errorf("BB0000002 Parcelles = %v, want two refs", b2.Parcelles)
	}
	s2 := sortStr(amberSignals(ptrEntry(b2)))
	if !eq(s2, []string{"no_active_mandate", "syndic_unknown"}) {
		t.Errorf("BB0000002 signals = %v", s2)
	}

	// Row 3: large + pre-1975 + QPV = the fragile archetype; bénévole on a
	// large copro; ANAH-aided; PDP perimeter flag set.
	c3 := by["CC0000003"]
	if !c3.CoproAidee {
		t.Errorf("CC0000003 CoproAidee = false, want true")
	}
	if !c3.DansPDP {
		t.Errorf("CC0000003 DansPDP = false, want true")
	}
	s3 := sortStr(amberSignals(ptrEntry(c3)))
	if !eq(s3, []string{"copro_aidee", "fragile_profile", "syndic_benevole"}) {
		t.Errorf("CC0000003 signals = %v", s3)
	}
	if c3.QPVName != "Grigny Centre" || c3.LotsTotal != 60 {
		t.Errorf("CC0000003 context wrong: %+v", c3)
	}

	// Row 4: bénévole but SMALL (8 lots) and no QPV — the size gate suppresses
	// the syndic signal and the fragile archetype. No signal at all.
	if got := amberSignals(ptrEntry(by["DD0000004"])); len(got) != 0 {
		t.Errorf("DD0000004 signals = %v, want none (small copro)", got)
	}
}

func ptrEntry(e Entry) *Entry { return &e }

func sortStr(s []string) []string { sort.Strings(s); return s }

func eq(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
