package rpls

import (
	"context"
	"errors"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
)

// TestLoad smokes the embedded dataset.
func TestLoad(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if idx == nil {
		t.Fatalf("nil index")
	}
	if got := idx.Count(); got < 30000 {
		t.Errorf("Count = %d, want ≥ 30000", got)
	}
	if idx.Meta.DataYear < 2023 {
		t.Errorf("Meta.DataYear = %d, want ≥ 2023", idx.Meta.DataYear)
	}
}

// TestQuery_LowRate_Neuilly pins a high-income inner-ring commune with a
// modest SRU rate.
func TestQuery_LowRate_Neuilly(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "92051"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Neuilly (92051)")
	}
	if res.LLSRate < 0 || res.LLSRate > 15 {
		t.Errorf("LLSRate = %v, want low (<15)", res.LLSRate)
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
}

// TestQuery_HighRate_Sevran pins a commune known for a very high social
// housing share.
func TestQuery_HighRate_Sevran(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "93071"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Sevran (93071)")
	}
	if res.LLSRate < 30 {
		t.Errorf("LLSRate = %v, want ≥ 30 (Sevran ~42 in 2024)", res.LLSRate)
	}
	if res.Tier != TierSatured {
		t.Errorf("Tier = %q, want %q", res.Tier, TierSatured)
	}
}

// TestQuery_ParisArrondissement folds 75118 onto 75056 and returns the
// Paris-wide rate.
func TestQuery_ParisArrondissement(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "75118"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for 75118 (Paris 18e)")
	}
	if res.Evidence.INSEE != "75056" {
		t.Errorf("Evidence.INSEE = %q, want 75056 (folded)", res.Evidence.INSEE)
	}
	if res.LLSRate <= 0 {
		t.Errorf("LLSRate = %v, want > 0", res.LLSRate)
	}
}

// TestQuery_UnknownCommune returns IsEmpty for a fake INSEE.
func TestQuery_UnknownCommune(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "99999"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil {
		t.Fatalf("nil result")
	}
	if !res.IsEmpty() {
		t.Errorf("IsEmpty = false, want true (99999 not in crosswalk)")
	}
	if res.Tier != TierUnknown {
		t.Errorf("Tier = %q, want %q", res.Tier, TierUnknown)
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

// TestQuery_InjectedIndex pins the classifier on a controlled fixture.
func TestQuery_InjectedIndex(t *testing.T) {
	t.Parallel()
	idx := &Index{
		Meta: Meta{Source: "test", DataYear: 2024, RowCountCommunes: 4},
		Communes: map[string]Entry{
			"10001": {Label: "Rural", RatePct: 1.5},
			"10002": {Label: "Mixte", RatePct: 8.0},
			"10003": {Label: "Fort", RatePct: 20.0},
			"10004": {Label: "Satured", RatePct: 55.0},
		},
	}
	cases := []struct {
		insee string
		want  Tier
		rate  float64
	}{
		{"10001", TierRural, 1.5},
		{"10002", TierMixte, 8.0},
		{"10003", TierFort, 20.0},
		{"10004", TierSatured, 55.0},
	}
	for _, c := range cases {
		res, err := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{INSEE: c.insee})
		if err != nil {
			t.Fatalf("Query(%s): %v", c.insee, err)
		}
		if res.LLSRate != c.rate {
			t.Errorf("INSEE %s LLSRate = %v, want %v", c.insee, res.LLSRate, c.rate)
		}
		if res.Tier != c.want {
			t.Errorf("INSEE %s Tier = %q, want %q", c.insee, res.Tier, c.want)
		}
	}
}

// TestClassify pins the thresholds.
func TestClassify(t *testing.T) {
	t.Parallel()
	cases := []struct {
		rate float64
		want Tier
	}{
		{0.0, TierRural},
		{2.99, TierRural},
		{3.0, TierMixte},
		{14.99, TierMixte},
		{15.0, TierFort},
		{29.99, TierFort},
		{30.0, TierSatured},
		{100.0, TierSatured},
	}
	for _, c := range cases {
		if got := classify(c.rate); got != c.want {
			t.Errorf("classify(%v) = %q, want %q", c.rate, got, c.want)
		}
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
