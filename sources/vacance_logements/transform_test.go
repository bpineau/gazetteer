package vacance_logements

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
)

// fixtureRawSet serves a single named file from testdata, implementing
// dataset.RawSet for the transform under test.
type fixtureRawSet struct{ path string }

func (f fixtureRawSet) Open(string) (io.ReadCloser, error) { return os.Open(f.path) }

func TestTransform_Golden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := transform(context.Background(), fixtureRawSet{"testdata/vacance_logements_sample.zip"}, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}

	// The rebuilt bytes must validate (gunzip + parse + non-empty) and parse.
	if err := validate(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validate: %v", err)
	}
	idx, err := parseIndex(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}

	// Golden values are the rounded counts + 2-decimal vacancy rate, taken
	// from the committed embedded blob. The 3-digit aggregate row and the
	// zero-LOG row must be dropped.
	want := map[string]Entry{
		"01001": {Log: 372, Vac: 17, RP: 341, RSec: 14, VacancyRatePct: 4.68},
		"01002": {Log: 175, Vac: 14, RP: 116, RSec: 45, VacancyRatePct: 8.15},
		"75056": {Log: 1396753, Vac: 132393, RP: 1128251, RSec: 136109, VacancyRatePct: 9.48},
		"13201": {Log: 24116, Vac: 1672, RP: 21208, RSec: 1236, VacancyRatePct: 6.93},
	}
	if idx.Count() != len(want) {
		t.Fatalf("count = %d, want %d (aggregate + zero-LOG rows must be skipped)", idx.Count(), len(want))
	}
	for insee, w := range want {
		got, ok := idx.Lookup(insee)
		if !ok || got != w {
			t.Errorf("%s: got (%+v, %v), want %+v", insee, got, ok, w)
		}
	}
	if _, ok := idx.Lookup("01999"); ok {
		t.Errorf("zero-LOG commune 01999 must be skipped")
	}

	// Meta is derived: count + static provenance/year.
	if idx.Meta.RowCountCommunes != len(want) {
		t.Errorf("RowCountCommunes = %d, want %d", idx.Meta.RowCountCommunes, len(want))
	}
	if idx.Meta.DataYear != dataYear {
		t.Errorf("DataYear = %d, want %d", idx.Meta.DataYear, dataYear)
	}
	if idx.Meta.Source != metaSource {
		t.Errorf("Source = %q, want %q", idx.Meta.Source, metaSource)
	}
}

func TestRound2(t *testing.T) {
	t.Parallel()
	cases := map[float64]float64{
		17.4456416337286 / 372.387493855914 * 100: 4.68,
		132392.854022185 / 1396753.13579177 * 100: 9.48,
	}
	for in, want := range cases {
		if got := round2(in); got != want {
			t.Errorf("round2(%v) = %v, want %v", in, got, want)
		}
	}
}
