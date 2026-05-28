package delinquance

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
	if got := idx.Count(); got < 20_000 {
		t.Errorf("Count = %d, want ≥ 20 000", got)
	}
	if idx.Meta.DataYear < 2020 {
		t.Errorf("DataYear = %d, want ≥ 2020", idx.Meta.DataYear)
	}
	if idx.Meta.Unit == "" {
		t.Errorf("Meta.Unit empty, want populated")
	}
}

// TestQuery_HappyPath_Paris pins a known commune and verifies at
// least one indicator is populated.
func TestQuery_HappyPath_Paris(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "75056"}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil || res.IsEmpty() {
		t.Fatalf("empty result for Paris (75056)")
	}
	if res.Confidence != ConfidenceHigh {
		t.Errorf("Confidence = %q, want %q", res.Confidence, ConfidenceHigh)
	}
	if len(res.Rates) == 0 {
		t.Errorf("Rates empty, want at least one indicator")
	}
	if res.Population <= 0 {
		t.Errorf("Population = %d, want > 0", res.Population)
	}
}

// TestQuery_UnknownCommune returns IsEmpty.
func TestQuery_UnknownCommune(t *testing.T) {
	t.Parallel()
	l := gazetteer.Listing{INSEE: "99999"}
	res, err := Query(context.Background(), Options{}, l)
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if res == nil {
		t.Fatalf("nil result, want non-nil empty")
	}
	if !res.IsEmpty() {
		t.Errorf("IsEmpty = false, want true")
	}
	if res.Flag != RiskUnknown {
		t.Errorf("Flag = %q, want %q", res.Flag, RiskUnknown)
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

// TestClassifyRisk pins the burglary-only three-bucket logic. The
// per-inhabitant indicators (theft_no_violence, vandalism) used to
// also trip RiskHigh, but they are denominator-inflated in tourist
// arrondissements (Paris 1er triple-tripped on theft=325 ‰ despite
// a real per-dwelling risk no higher than other Paris arrondissements).
// classifyRisk now anchors on burglary, which is per 1 000 logements.
func TestClassifyRisk(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		rates map[string]float64
		want  RiskFlag
	}{
		{"empty", map[string]float64{}, RiskUnknown},
		{"all-low", map[string]float64{"burglary": 1.0, "theft_no_violence": 4.0, "vandalism": 2.0}, RiskLow},
		{"medium-burglary", map[string]float64{"burglary": 4.0, "theft_no_violence": 10.0, "vandalism": 7.0}, RiskMedium},
		{"high-burglary", map[string]float64{"burglary": 7.0, "theft_no_violence": 3.0, "vandalism": 2.0}, RiskHigh},
		// Vandalism alone no longer trips high — burglary stays low.
		{"high-vandalism-only-stays-low", map[string]float64{"burglary": 2.0, "theft_no_violence": 5.0, "vandalism": 15.0}, RiskLow},
		// Theft alone (typical tourist district) no longer trips high — burglary stays medium.
		{"high-theft-only-stays-medium", map[string]float64{"burglary": 4.0, "theft_no_violence": 100.0, "vandalism": 5.0}, RiskMedium},
	}
	for _, c := range cases {
		if got := classifyRisk(c.rates); got != c.want {
			t.Errorf("%s: classifyRisk = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestHasInflatedPerInhabitantRates checks the heuristic that flags
// communes where ambient (daytime / tourist) population is much
// larger than the resident population used as the SSMSI denominator.
func TestHasInflatedPerInhabitantRates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		insee string
		want  bool
	}{
		{"75101", true},  // Paris 1er
		{"75120", true},  // Paris 20e
		{"75056", false}, // Paris (parent commune, undivided)
		{"69381", true},  // Lyon 1er
		{"69389", true},  // Lyon 9e
		{"69123", false}, // Lyon (parent)
		{"13201", true},  // Marseille 1er
		{"13216", true},  // Marseille 16e
		{"13055", false}, // Marseille (parent)
		{"75999", false}, // out-of-range Paris suffix
		{"92012", false}, // Courbevoie — La Défense not yet covered
		{"95100", false}, // Argenteuil
		{"15300", false}, // Murat
		{"", false},      // empty
		{"abc", false},   // garbage
	}
	for _, c := range cases {
		if got := hasInflatedPerInhabitantRates(c.insee); got != c.want {
			t.Errorf("hasInflatedPerInhabitantRates(%q) = %v, want %v", c.insee, got, c.want)
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
