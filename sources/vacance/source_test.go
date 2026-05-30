package vacance

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
	if idx.Meta.DataYear < 2020 {
		t.Errorf("Meta.DataYear = %d, want ≥ 2020", idx.Meta.DataYear)
	}
}

// TestQuery_SaintEtienne pins a commune known for high vacancy (déprise).
func TestQuery_SaintEtienne(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "42218"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Saint-Étienne (42218)")
	}
	if res.VacancyRate < 8 {
		t.Errorf("VacancyRate = %v, want ≥ 8 (Saint-Étienne ~12)", res.VacancyRate)
	}
	if res.Tier != TierEleve && res.Tier != TierDeprise {
		t.Errorf("Tier = %q, want élevé or déprise", res.Tier)
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
}

// TestQuery_Sevran pins a commune known for very-low vacancy (tendu).
func TestQuery_Sevran(t *testing.T) {
	t.Parallel()
	res, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "93071"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Sevran (93071)")
	}
	if res.VacancyRate > 8 {
		t.Errorf("VacancyRate = %v, want low (<8) — Sevran tendu", res.VacancyRate)
	}
}

// TestQuery_ParisArrondissement_NoFold ensures Paris arrondissements
// carry their own rows — fundamentally different from rpls/chomage.
func TestQuery_ParisArrondissement_NoFold(t *testing.T) {
	t.Parallel()
	res1, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "75101"})
	if err != nil {
		t.Fatalf("Query 75101: %v", err)
	}
	res18, err := Query(context.Background(), Options{}, gazetteer.Listing{INSEE: "75118"})
	if err != nil {
		t.Fatalf("Query 75118: %v", err)
	}
	if res1 == nil || res1.IsEmpty() {
		t.Fatalf("empty result for Paris 1er (75101)")
	}
	if res18 == nil || res18.IsEmpty() {
		t.Fatalf("empty result for Paris 18e (75118)")
	}
	if res1.Evidence.INSEE != "75101" {
		t.Errorf("Paris 1er Evidence.INSEE = %q, want 75101 (NOT folded)", res1.Evidence.INSEE)
	}
	if res18.Evidence.INSEE != "75118" {
		t.Errorf("Paris 18e Evidence.INSEE = %q, want 75118 (NOT folded)", res18.Evidence.INSEE)
	}
	// They differ in vacancy rate — even narrowly — which is the whole
	// point of keeping per-arrondissement granularity.
	if res1.VacancyRate == res18.VacancyRate {
		t.Errorf("Paris 1er and 18e have identical vacancy %v — fold leaked", res1.VacancyRate)
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
		Meta: Meta{Source: "test", DataYear: 2021, RowCountCommunes: 4},
		Communes: map[string]Entry{
			"10001": {Log: 100, Vac: 2, RP: 95, RSec: 3, VacancyRatePct: 2.0},    // tendu
			"10002": {Log: 100, Vac: 6, RP: 90, RSec: 4, VacancyRatePct: 6.0},    // normal
			"10003": {Log: 100, Vac: 11, RP: 85, RSec: 4, VacancyRatePct: 11.0},  // élevé
			"10004": {Log: 100, Vac: 20, RP: 70, RSec: 10, VacancyRatePct: 20.0}, // déprise
		},
	}
	cases := []struct {
		insee string
		want  Tier
		rate  float64
	}{
		{"10001", TierTendu, 2.0},
		{"10002", TierNormal, 6.0},
		{"10003", TierEleve, 11.0},
		{"10004", TierDeprise, 20.0},
	}
	for _, c := range cases {
		res, err := Query(context.Background(), Options{Index: idx}, gazetteer.Listing{INSEE: c.insee})
		if err != nil {
			t.Fatalf("Query(%s): %v", c.insee, err)
		}
		if res.VacancyRate != c.rate {
			t.Errorf("INSEE %s VacancyRate = %v, want %v", c.insee, res.VacancyRate, c.rate)
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
		{0.0, TierTendu},
		{3.99, TierTendu},
		{4.0, TierNormal},
		{7.99, TierNormal},
		{8.0, TierEleve},
		{14.99, TierEleve},
		{15.0, TierDeprise},
		{50.0, TierDeprise},
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
