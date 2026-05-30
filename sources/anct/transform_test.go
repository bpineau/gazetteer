package anct

import (
	"bytes"
	"context"
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

	if err := validate(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validate: %v", err)
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
