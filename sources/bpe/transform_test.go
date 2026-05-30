package bpe

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
	if idx.Meta.Source != metaSource {
		t.Errorf("Source = %q, want %q", idx.Meta.Source, metaSource)
	}
	if idx.Meta.ReferenceDate != referenceDate {
		t.Errorf("ReferenceDate = %q, want %q", idx.Meta.ReferenceDate, referenceDate)
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
