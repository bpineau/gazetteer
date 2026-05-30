package ips_ecoles

import (
	"bytes"
	"context"
	"io"
	"math"
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
	if err := transform(context.Background(), fixtureRawSet{"testdata/ips_ecoles_sample.csv"}, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}

	// The rebuilt bytes are gzipped JSON: validate and parse them through the
	// same gunzip path the runtime loader uses.
	if err := validate(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validate: %v", err)
	}
	idx, err := parseIndex(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}

	// Only rentrée 2024-2025 rows survive; the NS-IPS row (94046) is dropped
	// entirely, and the 2023-2024 / 2022-2023 rows are ignored.
	want := map[string]Entry{
		"01001": {IPSMedian: 110.3, IPSMin: 110.3, IPSMax: 110.3, SchoolCount: 1},
		"01014": {IPSMedian: 87.95, IPSMin: 80.6, IPSMax: 95.3, SchoolCount: 2}, // even count → avg of two central
		"75101": {IPSMedian: 130.0, IPSMin: 120.0, IPSMax: 140.0, SchoolCount: 3},
	}
	if idx.Count() != len(want) {
		t.Fatalf("count = %d, want %d (NS row + off-year rows must be skipped)", idx.Count(), len(want))
	}
	for insee, w := range want {
		got, ok := idx.Lookup(insee)
		if !ok {
			t.Errorf("%s: missing", insee)
			continue
		}
		if !approx(got.IPSMedian, w.IPSMedian) || !approx(got.IPSMin, w.IPSMin) ||
			!approx(got.IPSMax, w.IPSMax) || got.SchoolCount != w.SchoolCount {
			t.Errorf("%s: got %+v, want %+v", insee, got, w)
		}
	}
	if _, ok := idx.Lookup("94046"); ok {
		t.Errorf("94046: NS-only commune must not appear")
	}

	// Meta is derived from the CSV.
	if idx.Meta.DataYearLabel != dataYearLabel {
		t.Errorf("DataYearLabel = %q, want %q", idx.Meta.DataYearLabel, dataYearLabel)
	}
	if idx.Meta.RowCountCommunes != len(want) {
		t.Errorf("RowCountCommunes = %d, want %d", idx.Meta.RowCountCommunes, len(want))
	}
	if idx.Meta.RowCountSchools != 6 {
		t.Errorf("RowCountSchools = %d, want 6", idx.Meta.RowCountSchools)
	}
	if idx.Meta.Source != metaSource {
		t.Errorf("Source = %q, want %q", idx.Meta.Source, metaSource)
	}
}

func TestMedian(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   []float64
		want float64
	}{
		{[]float64{110.3}, 110.3},
		{[]float64{95.3, 80.6}, 87.95},  // even, unsorted
		{[]float64{140, 120, 130}, 130}, // odd
		{[]float64{1, 2, 3, 4}, 2.5},    // even
	}
	for _, c := range cases {
		if got := median(c.in); !approx(got, c.want) {
			t.Errorf("median(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestParseIPS(t *testing.T) {
	t.Parallel()
	if v, ok := parseIPS("110.3"); !ok || !approx(v, 110.3) {
		t.Errorf(`parseIPS("110.3") = (%v,%v)`, v, ok)
	}
	if v, ok := parseIPS("99,9"); !ok || !approx(v, 99.9) {
		t.Errorf(`parseIPS("99,9") = (%v,%v)`, v, ok)
	}
	for _, s := range []string{"NS", "", "  ", "n/a"} {
		if _, ok := parseIPS(s); ok {
			t.Errorf("parseIPS(%q) unexpectedly ok", s)
		}
	}
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
