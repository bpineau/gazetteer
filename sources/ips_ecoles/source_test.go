package ips_ecoles

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
	if got := idx.Count(); got < 10000 {
		t.Errorf("Count = %d, want ≥ 10000", got)
	}
	if idx.Meta.RowCountSchools < 20000 {
		t.Errorf("RowCountSchools = %d, want ≥ 20000", idx.Meta.RowCountSchools)
	}
	if idx.Meta.DataYearLabel == "" {
		t.Errorf("Meta.DataYearLabel empty")
	}
}

// TestQuery_FavoriseNeuilly pins a high-income inner-ring commune.
func TestQuery_FavoriseNeuilly(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "92051"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Neuilly (92051)")
	}
	if res.IPSMedian < 120 {
		t.Errorf("IPSMedian = %v, want ≥ 120 (Neuilly favorisé)", res.IPSMedian)
	}
	if res.Tier != TierFavorise {
		t.Errorf("Tier = %q, want %q", res.Tier, TierFavorise)
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q (Neuilly has many schools)", res.Confidence, ConfidenceHigh)
	}
}

// TestQuery_PrecaireSevran pins a commune known for a low IPS.
func TestQuery_PrecaireSevran(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "93071"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Sevran (93071)")
	}
	if res.IPSMedian > 90 {
		t.Errorf("IPSMedian = %v, want < 90 (Sevran low IPS)", res.IPSMedian)
	}
	if res.Tier != TierPrecaire && res.Tier != TierMixte {
		t.Errorf("Tier = %q, want precaire or mixte", res.Tier)
	}
}

// TestQuery_ParisArrondissementsDiffer ensures Paris arrondissements
// carry their own median — the load-bearing feature of this source.
func TestQuery_ParisArrondissementsDiffer(t *testing.T) {
	t.Parallel()
	res1, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "75101"})
	if err != nil {
		t.Fatalf("Query 75101: %v", err)
	}
	res18, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "75118"})
	if err != nil {
		t.Fatalf("Query 75118: %v", err)
	}
	res16, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "75116"})
	if err != nil {
		t.Fatalf("Query 75116: %v", err)
	}
	if res1.IsEmpty() || res18.IsEmpty() || res16.IsEmpty() {
		t.Fatalf("at least one Paris arrondissement empty: 75101=%v 75116=%v 75118=%v",
			res1.IsEmpty(), res16.IsEmpty(), res18.IsEmpty())
	}
	// The 16e and 1er should be markedly higher than the 18e.
	if res16.IPSMedian <= res18.IPSMedian {
		t.Errorf("75116 IPS %v ≤ 75118 IPS %v — expected stark difference", res16.IPSMedian, res18.IPSMedian)
	}
	if res1.Evidence.INSEE != "75101" {
		t.Errorf("Paris 1er Evidence.INSEE = %q, want 75101 (NOT folded)", res1.Evidence.INSEE)
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
		t.Errorf("IsEmpty = false, want true")
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
		Meta: Meta{Source: "test", DataYearLabel: "2024-2025", RowCountCommunes: 5, RowCountSchools: 20},
		Communes: map[string]Entry{
			"10001": {IPSMedian: 75.0, SchoolCount: 4},  // precaire
			"10002": {IPSMedian: 90.0, SchoolCount: 4},  // mixte
			"10003": {IPSMedian: 105.0, SchoolCount: 4}, // moyen
			"10004": {IPSMedian: 130.0, SchoolCount: 4}, // favorise
			"10005": {IPSMedian: 105.0, SchoolCount: 1}, // moyen but low confidence
		},
	}
	cases := []struct {
		insee    string
		want     Tier
		median   float64
		wantConf string
	}{
		{"10001", TierPrecaire, 75.0, ConfidenceHigh},
		{"10002", TierMixte, 90.0, ConfidenceHigh},
		{"10003", TierMoyen, 105.0, ConfidenceHigh},
		{"10004", TierFavorise, 130.0, ConfidenceHigh},
		{"10005", TierMoyen, 105.0, ConfidenceMedium},
	}
	for _, c := range cases {
		res, err := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{INSEE: c.insee})
		if err != nil {
			t.Fatalf("Query(%s): %v", c.insee, err)
		}
		if res.IPSMedian != c.median {
			t.Errorf("INSEE %s IPSMedian = %v, want %v", c.insee, res.IPSMedian, c.median)
		}
		if res.Tier != c.want {
			t.Errorf("INSEE %s Tier = %q, want %q", c.insee, res.Tier, c.want)
		}
		if res.Confidence != c.wantConf {
			t.Errorf("INSEE %s Confidence = %q, want %q", c.insee, res.Confidence, c.wantConf)
		}
	}
}

// TestClassify pins the thresholds.
func TestClassify(t *testing.T) {
	t.Parallel()
	cases := []struct {
		median float64
		want   Tier
	}{
		{50.0, TierPrecaire},
		{79.99, TierPrecaire},
		{80.0, TierMixte},
		{94.99, TierMixte},
		{95.0, TierMoyen},
		{119.99, TierMoyen},
		{120.0, TierFavorise},
		{200.0, TierFavorise},
	}
	for _, c := range cases {
		if got := classify(c.median); got != c.want {
			t.Errorf("classify(%v) = %q, want %q", c.median, got, c.want)
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
