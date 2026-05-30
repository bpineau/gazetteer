package rpls

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
	if err := transform(context.Background(), fixtureRawSet{"testdata/rpls_sample.csv"}, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}

	// The rebuilt bytes are gzipped JSON: they must validate and parse via the
	// real (gunzipping) parser.
	if err := validate(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validate: %v", err)
	}
	idx, err := parseIndex(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}

	want := map[string]Entry{
		"01001": {Label: "L'Abergement-Clémenciat", RatePct: 7.0},
		"01004": {Label: "Ambérieu-en-Bugey", RatePct: 30.0},
		"01002": {Label: "L'Abergement-de-Varey", RatePct: 0.0}, // comma decimal
		"75056": {Label: "Paris", RatePct: 35.0},
	}
	// Blank-INSEE and blank-rate rows are skipped.
	if idx.Count() != len(want) {
		t.Fatalf("count = %d, want %d (blank-INSEE and blank-rate rows must be skipped)", idx.Count(), len(want))
	}
	for insee, w := range want {
		got, ok := idx.Lookup(insee)
		if !ok {
			t.Errorf("%s: missing", insee)
			continue
		}
		if got.Label != w.Label || got.RatePct != w.RatePct {
			t.Errorf("%s: got %+v, want %+v", insee, got, w)
		}
	}

	// Meta is derived: count, fixed vintage and provenance string.
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

func TestParseRate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"7.0", 7.0, true},
		{"0,0", 0.0, true},
		{" 14 ", 14.0, true},
		{"30.04", 30.0, true},
		{"", 0, false},
		{"n/a", 0, false},
	}
	for _, c := range cases {
		got, ok := parseRate(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseRate(%q) = (%v,%v), want (%v,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}
