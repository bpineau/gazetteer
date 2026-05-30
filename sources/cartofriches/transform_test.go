package cartofriches

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
	if err := transform(context.Background(), fixtureRawSet{"testdata/friches_sample.csv"}, &buf); err != nil {
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

	// Two rows carry blank / NA INSEE and must be dropped: 3 communes,
	// 5 sites survive.
	if idx.Count() != 3 {
		t.Fatalf("Count = %d, want 3 (blank/NA INSEE rows must be skipped)", idx.Count())
	}
	if idx.Meta.RowCountCommunes != 3 {
		t.Errorf("RowCountCommunes = %d, want 3", idx.Meta.RowCountCommunes)
	}
	if idx.Meta.RowCountSites != 5 {
		t.Errorf("RowCountSites = %d, want 5", idx.Meta.RowCountSites)
	}
	if idx.Meta.Source != metaSource {
		t.Errorf("Source = %q, want %q", idx.Meta.Source, metaSource)
	}

	want := map[string]Entry{
		// Reproduces the committed snapshot's 61386 row byte-for-byte:
		// 2 habitat sites, both sans projet, summed UF surface.
		"61386": {
			Label:          "Saint-Evroult-Notre-Dame-du-Bois",
			SiteCount:      2,
			ByType:         map[string]int{"friche d'habitat": 2},
			ByStatus:       map[string]int{"friche sans projet": 2},
			TotalSurfaceM2: 13014,
		},
		// Mixed types; the second site's surface is NA and must not be
		// summed (only the first 20746 counts).
		"80126": {
			Label:          "BOUTTENCOURT",
			SiteCount:      2,
			ByType:         map[string]int{"mixte": 1, "inconnu": 1},
			ByStatus:       map[string]int{"friche sans projet": 2},
			TotalSurfaceM2: 20746,
		},
		// NA type + NA status + NA surface: counted as a site, but no
		// breakdown entries and zero surface (omitempty drops the maps).
		"99999": {
			Label:          "BLANK-SURFACE-NA-TYPE",
			SiteCount:      1,
			ByType:         nil,
			ByStatus:       nil,
			TotalSurfaceM2: 0,
		},
	}
	for insee, w := range want {
		got, ok := idx.Lookup(insee)
		if !ok {
			t.Errorf("%s: missing from index", insee)
			continue
		}
		if !reflect.DeepEqual(got, w) {
			t.Errorf("%s:\n got  %+v\n want %+v", insee, got, w)
		}
	}
}

func TestParseSurface(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"7089", 7089, true},
		{"  42 ", 42, true},
		{"NA", 0, false},
		{"", 0, false},
		{"160728.0", 160728, true},
		{"-3", 0, false},
		{"abc", 0, false},
	}
	for _, c := range cases {
		got, ok := parseSurface(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("parseSurface(%q) = (%d,%v), want (%d,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}
