package bpe

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestLoad smokes the embedded dataset.
func TestLoad(t *testing.T) {
	t.Parallel()
	idx, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if idx == nil {
		t.Fatalf("nil index")
	}
	if got := idx.Count(); got < 15000 {
		t.Errorf("Count = %d, want ≥ 15000 (~21 690 communes expected)", got)
	}
	if idx.Meta.ReferenceDate == "" {
		t.Errorf("Meta.ReferenceDate empty")
	}
	// At least one bucket total should be non-zero.
	total := 0
	for _, v := range idx.Meta.BucketTotals {
		total += v
	}
	if total == 0 {
		t.Errorf("Meta.BucketTotals all zero")
	}
}

// TestQuery_HappyPath_Paris pins Paris (75056) — aggregated at the
// parent commune level, so the global INSEE carries every facility
// (BPE does not split Paris into arrondissements).
func TestQuery_HappyPath_Paris(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "75056"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for 75056 Paris")
	}
	if res.TotalFacilities < 1000 {
		t.Errorf("TotalFacilities = %d, want ≥ 1000 for Paris", res.TotalFacilities)
	}
	if res.Get(BucketBoulangerie) == 0 {
		t.Errorf("expected at least one boulangerie in Paris")
	}
	if res.Get(BucketPharmacie) == 0 {
		t.Errorf("expected at least one pharmacie in Paris")
	}
	if res.Get(BucketMedecinGeneraliste) == 0 {
		t.Errorf("expected at least one médecin généraliste in Paris")
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
	if res.Evidence.ReferenceDate == "" {
		t.Errorf("Evidence.ReferenceDate empty")
	}
}

// TestQuery_RuralCommune pins a small rural commune that should still
// carry at least one curated facility (école primaire is universal in
// communes > 100 hab).
func TestQuery_RuralCommune(t *testing.T) {
	t.Parallel()
	// 01053 Bourg-en-Bresse — preset large enough to carry every
	// bucket.
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "01053"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for 01053 Bourg-en-Bresse")
	}
	if res.Get(BucketMedecinGeneraliste) == 0 {
		t.Errorf("expected ≥ 1 médecin généraliste in Bourg-en-Bresse")
	}
}

// TestQuery_UnknownCommune returns IsEmpty for an out-of-dataset code.
func TestQuery_UnknownCommune(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "99999"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil {
		t.Fatalf("nil result, want non-nil empty")
	}
	if !res.IsEmpty() {
		t.Errorf("IsEmpty = false, want true (99999 not in dataset)")
	}
	if res.Confidence != ConfidenceNone {
		t.Errorf("Confidence = %q, want empty", res.Confidence)
	}
}

// TestQuery_InsufficientInputs rejects empty INSEE.
func TestQuery_InsufficientInputs(t *testing.T) {
	t.Parallel()
	_, err := Query(context.Background(), Options{}, gazetteer.Listing{})
	if !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Fatalf("err = %v, want ErrInsufficientInputs", err)
	}
}

// TestQuery_InjectedIndex pins payload shape against a controlled
// fixture so unit logic does not drift with future data refreshes.
func TestQuery_InjectedIndex(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Meta: Meta{
			Source:           "test",
			ReferenceDate:    "2024-01-01",
			RowCountCommunes: 2,
		},
		Communes: map[string]map[Bucket]int{
			"10001": {
				BucketBoulangerie:        3,
				BucketPharmacie:          2,
				BucketMedecinGeneraliste: 5,
			},
			"10002": {
				// All zero - should fold to IsEmpty.
				BucketBoulangerie: 0,
			},
		},
	}
	res, err := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{INSEE: "10001"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.TotalFacilities != 10 {
		t.Errorf("TotalFacilities = %d, want 10", res.TotalFacilities)
	}
	if res.Get(BucketBoulangerie) != 3 {
		t.Errorf("Boulangerie = %d, want 3", res.Get(BucketBoulangerie))
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}

	// Zero-row commune folds to empty.
	res, err = Query(context.Background(), Options{Index: idx}, gazetteer.Listing{INSEE: "10002"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if !res.IsEmpty() {
		t.Errorf("IsEmpty = false, want true on zero-count row")
	}
}

// TestSourceRegistered ensures the init() side-effect wired the
// gazetteer registry.
func TestSourceRegistered(t *testing.T) {
	t.Parallel()
	if got := gazetteer.Lookup(Name); got == nil {
		t.Fatalf("gazetteer.Lookup(%q) = nil, want factory", Name)
	}
}

// TestSource_NameVersion smokes the Source interface adapter.
func TestSource_NameVersion(t *testing.T) {
	t.Parallel()
	s := NewSource(Options{})
	if s.Name() != Name {
		t.Errorf("Name() = %q, want %q", s.Name(), Name)
	}
	if s.Version() != sourceVersion {
		t.Errorf("Version() = %d, want %d", s.Version(), sourceVersion)
	}
}

// TestAllBucketsCoverage ensures the AllBuckets slice stays in sync
// with the public Bucket constants — drift would silently exclude a
// bucket from any renderer that iterates the list.
func TestAllBucketsCoverage(t *testing.T) {
	t.Parallel()
	want := map[Bucket]bool{
		BucketPoste: true, BucketGrandeSurface: true, BucketSuperette: true,
		BucketBoulangerie: true, BucketEcolePrimaire: true, BucketCollege: true,
		BucketLycee: true, BucketStructureSante: true, BucketMedecinGeneraliste: true,
		BucketInfirmier: true, BucketPharmacie: true, BucketCreche: true,
		BucketGare: true, BucketSportSalle: true, BucketSportPiscine: true,
		BucketSportTerrain: true,
	}
	if len(AllBuckets) != len(want) {
		t.Errorf("len(AllBuckets) = %d, want %d", len(AllBuckets), len(want))
	}
	for _, b := range AllBuckets {
		if !want[b] {
			t.Errorf("AllBuckets contains unknown bucket %q", b)
		}
		delete(want, b)
	}
	for missing := range want {
		t.Errorf("AllBuckets missing bucket %q", missing)
	}
}
