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
	idx, err := Load("")
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

// TestIndex_Level verifies the exported Level method and RiskFlag.String().
func TestIndex_Level(t *testing.T) {
	t.Parallel()
	idx, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Unknown INSEE → "unknown"
	if got := idx.Level("99999"); got.String() != "unknown" {
		t.Fatalf("Level(99999).String() = %q, want \"unknown\"", got.String())
	}
	// A Paris arrondissement (75101) → "unknown" because per-inhabitant rates
	// are inflated for arrondissement-split cities (ambient population effect).
	if got := idx.Level("75101"); got.String() != "unknown" {
		t.Fatalf("Level(75101).String() = %q, want \"unknown\" (inflated per-inhabitant rates)", got.String())
	}
	// A real commune — Neuilly-sur-Seine (92051) — should produce a valid level
	lvl := idx.Level("92051")
	if lvl.String() == "" {
		t.Fatal("Level(92051).String() is empty, want non-empty")
	}
	switch lvl.String() {
	case "low", "medium", "high", "unknown":
		// OK
	default:
		t.Fatalf("Level(92051).String() = %q, want one of low/medium/high/unknown", lvl.String())
	}
}

// TestRiskFlag_String verifies the String() method on every constant.
func TestRiskFlag_String(t *testing.T) {
	cases := []struct {
		flag RiskFlag
		want string
	}{
		{RiskUnknown, "unknown"},
		{RiskLow, "low"},
		{RiskMedium, "medium"},
		{RiskHigh, "high"},
	}
	for _, c := range cases {
		if got := c.flag.String(); got != c.want {
			t.Errorf("RiskFlag(%q).String() = %q, want %q", c.flag, got, c.want)
		}
	}
}

// TestClassifyRisk pins the social-distress three-bucket logic.
// Inputs are drug-trafficking, street-violence and unarmed-robbery
// rates (all per 1 000 inhabitants); burglary is intentionally
// IGNORED because luxury / tourist areas score highest on it (anti-
// signal). Each scenario name lists the kind of commune the case
// represents, so a future maintainer can spot a calibration shift.
func TestClassifyRisk(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		rates map[string]float64
		want  RiskFlag
	}{
		{
			"empty",
			map[string]float64{},
			RiskUnknown,
		},
		{
			"neuilly-like: low across the board",
			map[string]float64{"drug_trafficking": 0.57, "violence_outside_family": 1.33, "robbery_unarmed": 0.81, "burglary": 6.51},
			RiskLow,
		},
		{
			"aulnay-3000: drug_trafficking trips high",
			map[string]float64{"drug_trafficking": 2.87, "violence_outside_family": 4.19, "robbery_unarmed": 1.82, "burglary": 7.68},
			RiskHigh,
		},
		{
			"courneuve-4000: street-violence trips high",
			map[string]float64{"drug_trafficking": 0.98, "violence_outside_family": 7.84, "robbery_unarmed": 4.69, "burglary": 6.49},
			RiskHigh,
		},
		{
			"mantes-la-jolie commune-level: just under combined thresholds",
			map[string]float64{"drug_trafficking": 1.47, "violence_outside_family": 4.69, "robbery_unarmed": 1.49, "burglary": 3.55},
			RiskMedium,
		},
		{
			"argenteuil: combined dt+vof trip high",
			map[string]float64{"drug_trafficking": 1.59, "violence_outside_family": 4.25, "robbery_unarmed": 1.86, "burglary": 7.11},
			RiskHigh,
		},
		{
			"burglary-only luxury (Neuilly): stays low",
			map[string]float64{"drug_trafficking": 0.5, "violence_outside_family": 1.0, "robbery_unarmed": 0.5, "burglary": 8.0},
			RiskLow,
		},
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
