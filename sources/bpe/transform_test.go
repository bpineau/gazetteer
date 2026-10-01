package bpe

import (
	"archive/zip"
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

// TestTransform_Golden rebuilds the index from a tiny BPE ZIP fixture and
// pins the aggregation rules: only GEO_OBJECT=COM rows count, FACILITY_TYPE
// codes outside the curated bucket map are dropped, _T total rows are
// ignored, multiple codes fold into one bucket (C107+C108 → ecole_primaire,
// E107+E108+E109 → gare, B104+B105 → grande_surface), ARM/DEP rows are
// excluded (Paris stays aggregated at the parent commune 75056), and
// communes with no curated facility (or only zero counts) are omitted.
func TestTransform_Golden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := transform(context.Background(), fixtureRawSet{"testdata/bpe_sample.zip"}, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}

	// The rebuilt bytes are gzipped JSON; they must validate and parse.
	if err := validate(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validate: %v", err)
	}
	idx, err := parseIndex(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}

	want := map[string]map[Bucket]int{
		"10001": {
			BucketPoste:         1, // A206
			BucketBoulangerie:   3, // B207
			BucketEcolePrimaire: 3, // C107(1) + C108(2)
			BucketPharmacie:     2, // D307
		},
		"10002": {
			BucketMedecinGeneraliste: 5, // D265
			BucketInfirmier:          9, // D281
		},
		"75056": {
			BucketGare:          4, // E107(1) + E108(2) + E109(1) — ARM row excluded
			BucketGrandeSurface: 3, // B104(1) + B105(2)
		},
	}
	if idx.Count() != len(want) {
		t.Fatalf("count = %d, want %d (10003 non-curated and 10004 zero-count must be dropped)", idx.Count(), len(want))
	}
	for insee, counts := range want {
		got, ok := idx.Lookup(insee)
		if !ok {
			t.Errorf("%s: missing from index", insee)
			continue
		}
		if !reflect.DeepEqual(got, counts) {
			t.Errorf("%s: counts = %v, want %v", insee, got, counts)
		}
	}

	// Communes excluded by the rules.
	if _, ok := idx.Lookup("10003"); ok {
		t.Errorf("10003 (only non-curated A129) must be absent")
	}
	if _, ok := idx.Lookup("10004"); ok {
		t.Errorf("10004 (only a zero-count curated type) must be absent")
	}

	// Meta is derived from the aggregation.
	if idx.Meta.RowCountCommunes != len(want) {
		t.Errorf("RowCountCommunes = %d, want %d", idx.Meta.RowCountCommunes, len(want))
	}
	// The vintage (2024 in the fixture) is read off the member name.
	if want := "INSEE BPE 2024: dénombrement des équipements (curated bucket subset)"; idx.Meta.Source != want {
		t.Errorf("Source = %q, want %q", idx.Meta.Source, want)
	}
	if idx.Meta.ReferenceDate != "2024-01-01" {
		t.Errorf("ReferenceDate = %q, want 2024-01-01", idx.Meta.ReferenceDate)
	}

	// BucketTotals sum across communes, excluding the dropped rows.
	wantTotals := map[string]int{
		"poste":               1,
		"boulangerie":         3,
		"ecole_primaire":      3,
		"pharmacie":           2,
		"medecin_generaliste": 5,
		"infirmier":           9,
		"gare":                4,
		"grande_surface":      3,
	}
	if !reflect.DeepEqual(idx.Meta.BucketTotals, wantTotals) {
		t.Errorf("BucketTotals = %v, want %v", idx.Meta.BucketTotals, wantTotals)
	}
}

// TestBucketMapMatchesDocOrder ensures every curated Bucket constant is
// reachable from at least one FACILITY_TYPE code — i.e. the transform's
// mapping covers the whole public bucket set, with no orphan bucket.
func TestBucketMapMatchesDocOrder(t *testing.T) {
	t.Parallel()
	covered := map[Bucket]bool{}
	for _, b := range bucketByFacilityType {
		covered[b] = true
	}
	for _, b := range AllBuckets {
		if !covered[b] {
			t.Errorf("bucket %q has no FACILITY_TYPE code in bucketByFacilityType", b)
		}
	}
	if len(covered) != len(AllBuckets) {
		t.Errorf("bucketByFacilityType maps to %d buckets, want %d (AllBuckets)", len(covered), len(AllBuckets))
	}
}

// zipOf builds an in-memory ZIP holding the named members (all with body).
func zipOf(t *testing.T, body string, names ...string) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr
}

// TestDataMember pins the vintage-tolerant member lookup: INSEE keeps the
// archive URL across vintages and renames the CSV inside (DS_BPE_2024_data
// became DS_BPE_2025_data, which broke the refresh once). Any year is
// accepted, the newest wins whatever the member order, and the metadata
// CSV is never taken for the data.
func TestDataMember(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		members  []string
		wantName string
		wantYear int
	}{
		{"2025 vintage", []string{"DS_BPE_2025_data.csv", "DS_BPE_2025_metadata.csv"}, "DS_BPE_2025_data.csv", 2025},
		{"metadata listed first", []string{"DS_BPE_2026_metadata.csv", "DS_BPE_2026_data.csv"}, "DS_BPE_2026_data.csv", 2026},
		{"newest of two", []string{"DS_BPE_2026_data.csv", "DS_BPE_2025_data.csv"}, "DS_BPE_2026_data.csv", 2026},
		{"nested folder", []string{"bpe/DS_BPE_2025_data.csv"}, "bpe/DS_BPE_2025_data.csv", 2025},
	}
	for _, tt := range tests {
		f, year, err := dataMember(zipOf(t, "x", tt.members...))
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if f.Name != tt.wantName || year != tt.wantYear {
			t.Errorf("%s: got %s / %d, want %s / %d", tt.name, f.Name, year, tt.wantName, tt.wantYear)
		}
	}
	if _, _, err := dataMember(zipOf(t, "x", "DS_BPE_2025_metadata.csv", "readme.txt")); err == nil {
		t.Error("an archive without a data member must fail loudly")
	}
}

// TestTennisCode guards the sport_terrain bucket against the athletics code:
// BPE's F107 is "Athlétisme", tennis is F103.
func TestTennisCode(t *testing.T) {
	t.Parallel()
	if bucketByFacilityType["F103"] != BucketSportTerrain {
		t.Errorf("F103 (Tennis) must map to %s", BucketSportTerrain)
	}
	if _, ok := bucketByFacilityType["F107"]; ok {
		t.Error("F107 is Athlétisme, not tennis: it must not feed sport_terrain")
	}
}
