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
	idx, err := parseIndex(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}
	if idx.Count() != 3 {
		t.Fatalf("Count = %d, want 3", idx.Count())
	}

	by := map[string]Entry{}
	for _, e := range idx.Copros {
		by[e.Immatriculation] = e
	}

	// Row 1: clean — no amber signal.
	if got := amberSignals(ptrEntry(by["AA0000001"])); len(got) != 0 {
		t.Errorf("AA0000001 signals = %v, want none", got)
	}
	// Voie normalized (street-type marker stripped, number dropped).
	if v := by["AA0000001"].VoieNorm; v == "" || v == "20 r de gramont" {
		t.Errorf("AA0000001 VoieNorm = %q, want normalized", v)
	}

	// Row 2: no mandate + empty syndic.
	s2 := sortStr(amberSignals(ptrEntry(by["BB0000002"])))
	if !eq(s2, []string{"no_active_mandate", "syndic_unknown"}) {
		t.Errorf("BB0000002 signals = %v", s2)
	}

	// Row 3: bénévole + copro aidée.
	c3 := by["CC0000003"]
	if !c3.CoproAidee {
		t.Errorf("CC0000003 CoproAidee = false, want true")
	}
	s3 := sortStr(amberSignals(ptrEntry(c3)))
	if !eq(s3, []string{"copro_aidee", "syndic_benevole"}) {
		t.Errorf("CC0000003 signals = %v", s3)
	}
	if c3.QPVName != "Grigny Centre" || c3.LotsTotal != 60 {
		t.Errorf("CC0000003 context wrong: %+v", c3)
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
