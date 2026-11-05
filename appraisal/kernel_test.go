package appraisal

import (
	"math"
	"testing"
)

// TestPricePerM2_InvalidWeightsAreExcluded pins that a zero weight switches a
// source off entirely (it does not corroborate the other one), and that a
// negative, NaN or infinite weight never reaches the mean.
func TestPricePerM2_InvalidWeightsAreExcluded(t *testing.T) {
	t.Parallel()
	d := buildDossier(map[string]fakeEntry{
		"a": {data: fakePriceEstimator{eurPerM2Cents: 100_000, confidence: ConfidenceHigh}},
		"b": {data: fakePriceEstimator{eurPerM2Cents: 200_000, confidence: ConfidenceHigh}},
	})
	for _, w := range []float64{0, -0.75, math.NaN(), math.Inf(1), math.Inf(-1)} {
		got := PricePerM2(d, PriceOptions{Weights: map[string]float64{"a": 1, "b": w}})
		if got.EurPerM2Cents != 100_000 {
			t.Errorf("weight %v: EurPerM2Cents = %d, want 100000 (b excluded)", w, got.EurPerM2Cents)
		}
		if got.Confidence != ConfidenceLow {
			t.Errorf("weight %v: Confidence = %v, want low (one contributing dataset)", w, got.Confidence)
		}
		for _, in := range got.Inputs {
			if in.Source == "b" && (!in.Excluded || in.ExcludedWhy != excludedWeight) {
				t.Errorf("weight %v: input b Excluded %v why %q, want %q", w, in.Excluded, in.ExcludedWhy, excludedWeight)
			}
		}
	}
}

func TestRentValue_InvalidWeightsAreExcluded(t *testing.T) {
	t.Parallel()
	d := buildDossier(map[string]fakeEntry{
		"a": {data: fakeRentEstimator{eurPerM2Cents: 20_00, confidence: ConfidenceHigh}},
		"b": {data: fakeRentEstimator{eurPerM2Cents: 40_00, confidence: ConfidenceHigh}},
	})
	for _, w := range []float64{0, -2, math.NaN(), math.Inf(1)} {
		// MinSources 2: with b excluded, a alone cannot reach it.
		got := RentValue(d, RentOptions{Weights: map[string]float64{"a": 1, "b": w}, MinSources: 2})
		if got.EurPerM2Cents != 20_00 || got.Confidence != ConfidenceLow {
			t.Errorf("weight %v: %d cents at %v, want 2000 at low", w, got.EurPerM2Cents, got.Confidence)
		}
	}
	// Every weight invalid: nothing to say.
	got := RentValue(d, RentOptions{Weights: map[string]float64{"a": 0, "b": 0}})
	if got.EurPerM2Cents != 0 {
		t.Errorf("all weights zero: %d cents, want 0", got.EurPerM2Cents)
	}
}
