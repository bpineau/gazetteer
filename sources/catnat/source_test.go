package catnat

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/appraisal"
	"github.com/bpineau/gazetteer/gazetteer"
)

// TestLoad smokes the embedded aggregate.
func TestLoad(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := idx.Count(); got < 30000 {
		t.Errorf("Count = %d, want ≥ 30000", got)
	}
	if idx.refYear < 2020 {
		t.Errorf("refYear = %d, want ≥ 2020", idx.refYear)
	}
}

// TestQuery_Commune resolves a commune with a known drought history.
func TestQuery_Commune(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "91471"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res.IsEmpty() {
		t.Fatalf("empty result for 91471")
	}
	if res.TotalArretes < 5 {
		t.Errorf("TotalArretes = %d, want ≥ 5", res.TotalArretes)
	}
	if res.ByCategory[CatSecheresse] == 0 {
		t.Errorf("expected sécheresse decrees in ByCategory, got %v", res.ByCategory)
	}
	if res.Tier == "" || res.Confidence != ConfidenceHigh {
		t.Errorf("tier/confidence = %q/%q", res.Tier, res.Confidence)
	}
	if res.LastEventYear < 2000 {
		t.Errorf("LastEventYear = %d", res.LastEventYear)
	}
	// HazardReport feeds appraisal.HazardProfile.
	rep := res.HazardReport()
	if !contains(rep.NaturalRisks, CatSecheresse) {
		t.Errorf("HazardReport.NaturalRisks = %v, want to include %q", rep.NaturalRisks, CatSecheresse)
	}
}

// TestQuery_FoldArrondissement resolves a Paris arrondissement to the mother
// commune (CatNat decrees are issued at commune level).
func TestQuery_FoldArrondissement(t *testing.T) {
	t.Parallel()
	arr, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "75112"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	mother, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "75056"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if arr.IsEmpty() || arr.TotalArretes != mother.TotalArretes {
		t.Errorf("Paris 12e total = %d, want = mother 75056 total %d", arr.TotalArretes, mother.TotalArretes)
	}
	if arr.Evidence.INSEE != "75056" {
		t.Errorf("Evidence.INSEE = %q, want 75056 (folded)", arr.Evidence.INSEE)
	}
}

// TestQuery_MissingINSEE skips without a commune code.
func TestQuery_MissingINSEE(t *testing.T) {
	t.Parallel()
	if _, err := Query(context.Background(), Options{}, gazetteer.Listing{}); !errors.Is(err, gazetteer.ErrInsufficientInputs) {
		t.Errorf("err = %v, want ErrInsufficientInputs", err)
	}
}

// TestHazardProfileIntegration confirms catnat contributes to the consolidated
// hazard view.
func TestHazardProfileIntegration(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "91471"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	d := gazetteer.Dossier{Results: map[string]gazetteer.Result{
		Name: {Name: Name, Status: gazetteer.StatusOK, Data: res},
	}}
	prof := appraisal.HazardProfile(d)
	if !contains(prof.NaturalRisks, CatSecheresse) {
		t.Errorf("HazardProfile.NaturalRisks = %v, want to include %q", prof.NaturalRisks, CatSecheresse)
	}
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
