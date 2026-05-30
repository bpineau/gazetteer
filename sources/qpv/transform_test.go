package qpv

import (
	"bytes"
	"context"
	"io"
	"os"
	"reflect"
	"testing"
)

// fixtureRawSet serves a single named file from testdata, implementing
// dataset.RawSet for the transform under test.
type fixtureRawSet struct{ path string }

func (f fixtureRawSet) Open(string) (io.ReadCloser, error) { return os.Open(f.path) }

func TestTransform_Golden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := transform(context.Background(), fixtureRawSet{"testdata/listeqp2024_sample.csv"}, &buf); err != nil {
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

	// Three communes: two single (one needing zero-pad) and one multi-commune.
	// The blank-code row is skipped.
	if idx.Count() != 3 {
		t.Fatalf("count = %d, want 3 (blank-code row must be skipped)", idx.Count())
	}
	if idx.Meta.RowCountCommunes != 3 {
		t.Errorf("RowCountCommunes = %d, want 3", idx.Meta.RowCountCommunes)
	}
	if idx.Meta.RowCountQPV != 4 {
		t.Errorf("RowCountQPV = %d, want 4", idx.Meta.RowCountQPV)
	}
	if idx.Meta.Source != metaSource {
		t.Errorf("Source = %q, want %q", idx.Meta.Source, metaSource)
	}

	want := map[string]Entry{
		// Low-dept code "1053" is zero-padded; the two QPVs are sorted by code
		// (input gave them out of order).
		"01053": {
			Label: "Bourg-en-Bresse",
			QPVs: []QPV{
				{Code: "QN00101M", Label: "Grande Reyssouze Terre Des Fleurs"},
				{Code: "QN00102M", Label: "Croix Blanche"},
			},
		},
		"01004": {
			Label: "Ambérieu-en-Bugey",
			QPVs:  []QPV{{Code: "QN00103M", Label: "Les Courbes De L'Albarine"}},
		},
		// Multi-commune QPV: insee_com kept verbatim as the key, lib_com as label.
		"02571; 02691": {
			Label: "Omissy; Saint-Quentin",
			QPVs:  []QPV{{Code: "QN00201M", Label: "Europe"}},
		},
	}
	for key, w := range want {
		got, ok := idx.Lookup(key)
		if !ok {
			t.Errorf("%q: not found", key)
			continue
		}
		if !reflect.DeepEqual(got, w) {
			t.Errorf("%q: got %+v, want %+v", key, got, w)
		}
	}
}

func TestCommuneKey(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"1053":         "01053",
		"01004":        "01004",
		" 1004 ":       "01004",
		"75056":        "75056",
		"02571; 02691": "02571; 02691",
		"":             "",
		"  ":           "",
	}
	for in, want := range cases {
		if got := communeKey(in); got != want {
			t.Errorf("communeKey(%q) = %q, want %q", in, got, want)
		}
	}
}
