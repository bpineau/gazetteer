package anct

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

// fixtureRawSet serves named raw files from testdata, implementing
// dataset.RawSet for the transform under test.
type fixtureRawSet struct{ dir string }

func (f fixtureRawSet) Open(name string) (io.ReadCloser, error) {
	return os.Open(filepath.Join(f.dir, name))
}

func TestTransform_Golden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := transform(context.Background(), fixtureRawSet{"testdata"}, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}

	// A four-commune fixture is far below the publication floors.
	if err := validate(bytes.NewReader(buf.Bytes())); err == nil {
		t.Error("validate must reject lists below their floors")
	}
	idx, err := parseIndex(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}

	// Four communes flagged; the blank-INSEE ACV row and the "Non signée"
	// ORT row are dropped.
	if idx.Count() != 4 {
		t.Fatalf("count = %d, want 4", idx.Count())
	}

	want := map[string]Entry{
		"26362": { // ACV + ORT; ACV date DD-MM-YYYY -> ISO
			Label: "Valence", ACV: true, ACVSignedAt: "2024-01-02",
			ORT: true, ORTSignedAt: "2020-02-27",
		},
		"01053": { // ACV only
			Label: "Bourg-en-Bresse", ACV: true, ACVSignedAt: "2024-01-25",
		},
		"01034": { // PVD + ORT
			Label: "Belley", PVD: true, PVDSignedAt: "2021-04-22",
			ORT: true, ORTSignedAt: "2022-11-21",
		},
		"75056": { // ACV + PVD, no ORT; ACV label wins
			Label: "Paris", ACV: true, ACVSignedAt: "2020-06-15",
			PVD: true, PVDSignedAt: "2021-09-09",
		},
	}
	for insee, w := range want {
		got, ok := idx.Lookup(insee)
		if !ok {
			t.Errorf("%s: missing", insee)
			continue
		}
		if got != w {
			t.Errorf("%s: got %+v, want %+v", insee, got, w)
		}
	}

	// The unsigned ORT commune (Lyon) must not appear.
	if _, ok := idx.Lookup("69123"); ok {
		t.Errorf("69123 (Non signée) should be absent")
	}

	// Meta counts.
	if idx.Meta.RowCountCommunes != 4 {
		t.Errorf("RowCountCommunes = %d, want 4", idx.Meta.RowCountCommunes)
	}
	if idx.Meta.RowCountACV != 3 {
		t.Errorf("RowCountACV = %d, want 3", idx.Meta.RowCountACV)
	}
	if idx.Meta.RowCountPVD != 2 {
		t.Errorf("RowCountPVD = %d, want 2", idx.Meta.RowCountPVD)
	}
	if idx.Meta.RowCountORT != 2 {
		t.Errorf("RowCountORT = %d, want 2", idx.Meta.RowCountORT)
	}
	if idx.Meta.Source != metaSource {
		t.Errorf("Source = %q, want %q", idx.Meta.Source, metaSource)
	}
}

func TestDMYToISO(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"11-03-2024": "2024-03-11",
		"02-01-2024": "2024-01-02",
		"22/04/2021": "2021-04-22", // PVD list since 2026
		"2021-04-22": "2021-04-22", // already ISO, untouched
		"":           "",
		"garbage":    "garbage",
	}
	for in, want := range cases {
		if got := dmyToISO(in); got != want {
			t.Errorf("dmyToISO(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestTransform_2026Shapes runs the 2026 upstream shapes: the PVD list's
// DD/MM/YYYY dates, and the ORT export of the raw Grist table (other column
// order, a multi-line remark, the "Terminée" status of an ended convention).
// The output must not change: same communes, ISO dates.
func TestTransform_2026Shapes(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := transform(context.Background(), fixtureRawSet{"testdata/2026"}, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}
	idx, err := parseIndex(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}
	want := map[string]Entry{
		"26362": {Label: "Valence", ACV: true, ACVSignedAt: "2024-01-02", ORT: true, ORTSignedAt: "2020-02-27"},
		"01053": {Label: "Bourg-en-Bresse", ACV: true, ACVSignedAt: "2024-01-25"},
		"01034": {Label: "Belley", PVD: true, PVDSignedAt: "2021-04-22", ORT: true, ORTSignedAt: "2022-11-21"},
		"75056": {Label: "Paris", ACV: true, ACVSignedAt: "2020-06-15", PVD: true, PVDSignedAt: "2021-09-09"},
	}
	if idx.Count() != len(want) {
		t.Errorf("count = %d, want %d (ended and unsigned ORT rows dropped)", idx.Count(), len(want))
	}
	for insee, w := range want {
		if got, ok := idx.Lookup(insee); !ok || got != w {
			t.Errorf("%s: got %+v (ok %v), want %+v", insee, got, ok, w)
		}
	}
	for _, insee := range []string{"69123", "29019"} {
		if _, ok := idx.Lookup(insee); ok {
			t.Errorf("%s: an ended or unsigned ORT convention must not flag the commune", insee)
		}
	}
}

// TestValidate_Floors pins the guard against a narrowed upstream export: the
// ORT list once came back with one département's rows only, and the rebuild
// published it. Lists at their floors pass, one list below fails.
func TestValidate_Floors(t *testing.T) {
	t.Parallel()
	build := func(acv, pvd, ort int) []byte {
		idx := Index{
			Meta:     Meta{Source: metaSource, RowCountCommunes: 1, RowCountACV: acv, RowCountPVD: pvd, RowCountORT: ort},
			Communes: map[string]Entry{"26362": {ACV: true}},
		}
		b, err := json.Marshal(idx)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	if err := validate(bytes.NewReader(build(minACV, minPVD, minORT))); err != nil {
		t.Errorf("lists at their floors: %v", err)
	}
	if err := validate(bytes.NewReader(build(minACV, minPVD, 77))); err == nil {
		t.Error("an ORT list of 77 communes must fail validation")
	}
}
