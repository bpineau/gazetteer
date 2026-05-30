package zonageabc

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
	if err := transform(context.Background(), fixtureRawSet{"testdata/zonage_abc_sample.csv"}, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}

	// The rebuilt bytes must validate and parse.
	if err := validate(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validate: %v", err)
	}
	idx, err := parseIndex(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}

	want := map[string]Zone{
		"01001": ZoneC,
		"01004": ZoneB1,
		"75056": ZoneAbis,
		"06088": ZoneA,
		"26362": ZoneB2,
	}
	if idx.Count() != len(want) {
		t.Fatalf("count = %d, want %d (blank-INSEE row must be skipped)", idx.Count(), len(want))
	}
	for insee, zone := range want {
		got, ok := idx.Lookup(insee)
		if !ok || got != zone {
			t.Errorf("%s: got (%q,%v), want %q", insee, got, ok, zone)
		}
	}

	// Meta is derived from the CSV: count, ISO effective date parsed from the
	// zone-column header, and the provenance string.
	if idx.Meta.RowCountCommunes != len(want) {
		t.Errorf("RowCountCommunes = %d, want %d", idx.Meta.RowCountCommunes, len(want))
	}
	if idx.Meta.EffectiveDate != "2025-09-05" {
		t.Errorf("EffectiveDate = %q, want 2025-09-05", idx.Meta.EffectiveDate)
	}
	if idx.Meta.Source != metaSource {
		t.Errorf("Source = %q, want %q", idx.Meta.Source, metaSource)
	}
}

func TestParseEffectiveDate(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"Zonage en vigueur depuis le 5 septembre 2025": "2025-09-05",
		"Zonage depuis le 1 août 2014":                 "2014-08-01",
		"no date here":                                 "",
	}
	for in, want := range cases {
		if got := parseEffectiveDate(in); got != want {
			t.Errorf("parseEffectiveDate(%q) = %q, want %q", in, got, want)
		}
	}
}
