package zonescore

import (
	"testing"

	"slices"

	"github.com/bpineau/gazetteer/appraisal"
	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/sources/delinquance"
	"github.com/bpineau/gazetteer/sources/dvf"
	"github.com/bpineau/gazetteer/sources/filosofi"
	"github.com/bpineau/gazetteer/sources/nuisances"
	"github.com/bpineau/gazetteer/sources/oll"
	"github.com/bpineau/gazetteer/sources/taxefonciere"
	"github.com/bpineau/gazetteer/sources/vacance"
)

func okResult(name string, data any) gazetteer.Result {
	return gazetteer.Result{Name: name, Status: gazetteer.StatusOK, Data: data}
}

func dossier(rs ...gazetteer.Result) gazetteer.Dossier {
	m := make(map[string]gazetteer.Result, len(rs))
	for _, r := range rs {
		m[r.Name] = r
	}
	return gazetteer.Dossier{Results: m}
}

func axisByName(s Score, name string) (Axis, bool) {
	for _, a := range s.Axes {
		if a.Name == name {
			return a, true
		}
	}
	return Axis{}, false
}

// TestScoreRendement checks the dominant axis: 18€/m²/mo over 4000€/m² = 5.4 %
// gross, which lerp(2,8) maps to ~56.7.
func TestScoreRendement(t *testing.T) {
	t.Parallel()
	d := dossier(
		okResult(dvf.Name, &dvf.Result{ValueEURPerM2Cents: new(int64(400000)), SampleSize: 10}),
		okResult(oll.Name, &oll.Result{ObservedMedianEURPerM2: 18, SampleSize: 100, Confidence: "high"}),
	)
	r := scoreRendement(d)
	if !r.present {
		t.Fatal("rendement axis not present with price+rent")
	}
	// yield 5.4 %, lerp(3,8) → (5.4-3)/5*100 = 48.
	if r.value < 46 || r.value > 50 {
		t.Errorf("rendement value = %.1f, want ~48 (5.4%% yield, 3–8%% band)", r.value)
	}
	if !slices.Contains(r.sources, dvf.Name) || !slices.Contains(r.sources, oll.Name) {
		t.Errorf("sources = %v, want dvf+oll", r.sources)
	}
}

// TestCompute_Composite scores a multi-source dossier and checks the breakdown.
func TestCompute_Composite(t *testing.T) {
	t.Parallel()
	d := dossier(
		okResult(dvf.Name, &dvf.Result{ValueEURPerM2Cents: new(int64(400000)), SampleSize: 10}),
		okResult(oll.Name, &oll.Result{ObservedMedianEURPerM2: 18, SampleSize: 100, Confidence: "high"}),
		okResult(filosofi.Name, &filosofi.Result{MedianEUR: 22000, Flag: filosofi.RiskMedium, Confidence: "high"}),
		okResult(delinquance.Name, &delinquance.Result{Flag: delinquance.RiskLow, Population: 50000, Confidence: "high", Rates: map[string]float64{"x": 1}}),
		okResult(nuisances.Name, &nuisances.Result{NuisanceCount: 1, Tier: nuisances.TierModere, Confidence: "high"}),
	)
	s := Compute(d)
	if s.Composite <= 0 || s.Composite > 100 {
		t.Fatalf("composite = %.1f, want 0<c≤100", s.Composite)
	}
	// rendement + solvabilite + securite + acces present; tension + fiscalite absent.
	for _, name := range []string{AxisRendement, AxisSolvabilite, AxisSecurite, AxisAcces} {
		if a, _ := axisByName(s, name); !a.Present {
			t.Errorf("axis %s should be present", name)
		}
	}
	for _, name := range []string{AxisTension, AxisFiscalite} {
		if a, _ := axisByName(s, name); a.Present {
			t.Errorf("axis %s should be absent", name)
		}
	}
	// securite (delinquance low) should score high (85).
	if a, _ := axisByName(s, AxisSecurite); a.Value != 85 {
		t.Errorf("securite value = %.1f, want 85 (low delinquance)", a.Value)
	}
	// Coverage 0.42+0.13+0.10+0.07 = 0.72 of 1.0 → Medium (≥0.5, <0.8).
	if s.Confidence != appraisal.ConfidenceMedium {
		t.Errorf("confidence = %v, want Medium (coverage 0.72)", s.Confidence)
	}
}

// TestCompute_Empty degrades to a zero composite at low confidence.
func TestCompute_Empty(t *testing.T) {
	t.Parallel()
	s := Compute(dossier())
	if s.Composite != 0 {
		t.Errorf("composite = %.1f, want 0 on empty dossier", s.Composite)
	}
	if s.Confidence != appraisal.ConfidenceLow {
		t.Errorf("confidence = %v, want Low", s.Confidence)
	}
	for _, a := range s.Axes {
		if a.Present {
			t.Errorf("axis %s present on empty dossier", a.Name)
		}
	}
}

// TestLerp pins the normalisation kernel, including the inverted (hi<lo) case
// and both clamps.
func TestLerp(t *testing.T) {
	t.Parallel()
	cases := []struct {
		x, lo, hi, want float64
	}{
		{5, 0, 10, 50},
		{0, 0, 10, 0},
		{10, 0, 10, 100},
		{-5, 0, 10, 0},    // clamp low
		{15, 0, 10, 100},  // clamp high
		{7, 7, 7, 50},     // degenerate
		{15, 55, 15, 100}, // inverted: lower x scores higher
		{55, 55, 15, 0},
		{35, 55, 15, 50},
	}
	for _, c := range cases {
		if got := lerp(c.x, c.lo, c.hi); got != c.want {
			t.Errorf("lerp(%v,%v,%v) = %v, want %v", c.x, c.lo, c.hi, got, c.want)
		}
	}
}

// TestCompute_HighConfidence: one source per axis → full coverage + rendement →
// High.
func TestCompute_HighConfidence(t *testing.T) {
	t.Parallel()
	d := dossier(
		okResult(dvf.Name, &dvf.Result{ValueEURPerM2Cents: new(int64(400000)), SampleSize: 10}),
		okResult(oll.Name, &oll.Result{ObservedMedianEURPerM2: 18, SampleSize: 100, Confidence: "high"}),
		okResult(vacance.Name, &vacance.Result{VacancyRate: 5, Confidence: "high"}),
		okResult(filosofi.Name, &filosofi.Result{MedianEUR: 22000, Flag: filosofi.RiskLow, Confidence: "high"}),
		okResult(delinquance.Name, &delinquance.Result{Flag: delinquance.RiskLow, Population: 1000, Confidence: "high", Rates: map[string]float64{"x": 1}}),
		okResult(taxefonciere.Name, &taxefonciere.Result{TauxTFPBApplied: 30, Confidence: "high"}),
		okResult(nuisances.Name, &nuisances.Result{NuisanceCount: 0, Tier: nuisances.TierCalme, Confidence: "high"}),
	)
	s := Compute(d)
	if s.Confidence != appraisal.ConfidenceHigh {
		t.Errorf("confidence = %v, want High (full coverage + rendement)", s.Confidence)
	}
}

// TestCompute_RendementGate: full coverage via custom weights but WITHOUT the
// rendement axis caps confidence at Medium.
func TestCompute_RendementGate(t *testing.T) {
	t.Parallel()
	d := dossier(
		okResult(delinquance.Name, &delinquance.Result{Flag: delinquance.RiskLow, Population: 1000, Confidence: "high", Rates: map[string]float64{"x": 1}}),
		okResult(nuisances.Name, &nuisances.Result{NuisanceCount: 0, Tier: nuisances.TierCalme, Confidence: "high"}),
	)
	// Both present, coverage 1.0 — but rendement is absent, so High is withheld.
	s := Compute(d, Options{Weights: map[string]float64{AxisSecurite: 0.5, AxisAcces: 0.5}})
	if s.Confidence != appraisal.ConfidenceMedium {
		t.Errorf("confidence = %v, want Medium (no rendement axis despite full coverage)", s.Confidence)
	}
}

// TestCompute_WeightOverride honours Options.Weights.
func TestCompute_WeightOverride(t *testing.T) {
	t.Parallel()
	d := dossier(
		okResult(delinquance.Name, &delinquance.Result{Flag: delinquance.RiskLow, Population: 1000, Confidence: "high", Rates: map[string]float64{"x": 1}}),
		okResult(nuisances.Name, &nuisances.Result{NuisanceCount: 3, Tier: nuisances.TierTresExpose, Confidence: "high"}),
	)
	// Only securite (85) and acces (15) present. Weight securite 3×, acces 1×.
	s := Compute(d, Options{Weights: map[string]float64{AxisSecurite: 0.3, AxisAcces: 0.1}})
	// (85*0.3 + 15*0.1) / 0.4 = (25.5+1.5)/0.4 = 67.5
	if s.Composite < 66 || s.Composite > 69 {
		t.Errorf("composite = %.1f, want ~67.5", s.Composite)
	}
}
