package appraisal

import "testing"

// fakeRentCapper contributes both a market reading and a legal cap (as
// encadrement's Result does). eurPerM2Cents is the blend contribution;
// capCents is the majoré ceiling.
type fakeRentCapper struct {
	eurPerM2Cents int64
	confidence    Confidence
	capCents      int64
	capOK         bool
}

func (f fakeRentCapper) RentEstimate() RentEstimate {
	return RentEstimate{EurPerM2Cents: f.eurPerM2Cents, Confidence: f.confidence, Bracket: "enc_zone"}
}
func (f fakeRentCapper) RentCap() (int64, bool) { return f.capCents, f.capOK }

var (
	_ RentEstimator = fakeRentCapper{}
	_ RentCapper    = fakeRentCapper{}
)

func TestEffectiveRentCents(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name             string
		blend, cap, want int64
	}{
		{"blend below cap → blend", 20_00, 30_00, 20_00},
		{"blend above cap → cap", 35_00, 30_00, 30_00},
		{"no cap → blend", 20_00, 0, 20_00},
		{"no blend → cap", 0, 30_00, 30_00},
		{"neither → 0", 0, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := EffectiveRentCents(c.blend, c.cap); got != c.want {
				t.Errorf("EffectiveRentCents(%d, %d) = %d, want %d", c.blend, c.cap, got, c.want)
			}
		})
	}
}

func TestRentValue_CapCollectedAndEffective(t *testing.T) {
	t.Parallel()

	// Market blend (25.00) sits below the legal cap (30.00): effective = blend.
	d := buildDossier(map[string]fakeEntry{
		"carteloyers": {data: fakeRentEstimator{eurPerM2Cents: 25_00, confidence: ConfidenceMedium}},
		"encadrement": {data: fakeRentCapper{eurPerM2Cents: 24_00, confidence: ConfidenceMedium, capCents: 30_00, capOK: true}},
	})
	got := RentValue(d)
	if got.CapEurPerM2Cents != 30_00 {
		t.Errorf("CapEurPerM2Cents = %d, want 3000", got.CapEurPerM2Cents)
	}
	if got.EurPerM2Cents <= 0 || got.EurPerM2Cents > 30_00 {
		t.Errorf("blend = %d, want a market blend below the cap", got.EurPerM2Cents)
	}
	if eff := got.EffectiveEURPerM2(); eff != float64(got.EurPerM2Cents)/100 {
		t.Errorf("EffectiveEURPerM2 = %.2f, want the blend %.2f (blend < cap)", eff, float64(got.EurPerM2Cents)/100)
	}
}

func TestRentValue_BlendClampedToCap(t *testing.T) {
	t.Parallel()
	// A hot market blend (36.00) exceeds the legal ceiling (30.00): the
	// effective rent must clamp to the cap.
	d := buildDossier(map[string]fakeEntry{
		"carteloyers": {data: fakeRentEstimator{eurPerM2Cents: 40_00, confidence: ConfidenceHigh}},
		"oll":         {data: fakeRentEstimator{eurPerM2Cents: 32_00, confidence: ConfidenceHigh}},
		"encadrement": {data: fakeRentCapper{eurPerM2Cents: 30_00, confidence: ConfidenceMedium, capCents: 30_00, capOK: true}},
	})
	got := RentValue(d)
	if got.CapEurPerM2Cents != 30_00 {
		t.Fatalf("CapEurPerM2Cents = %d, want 3000", got.CapEurPerM2Cents)
	}
	if got.EurPerM2Cents <= 30_00 {
		t.Fatalf("blend = %d, want a blend above the cap for this test", got.EurPerM2Cents)
	}
	if got.EffectiveEURPerM2() != 30.0 {
		t.Errorf("EffectiveEURPerM2 = %.2f, want 30.00 (clamped to the legal cap)", got.EffectiveEURPerM2())
	}
}

func TestRentValue_CapOnlyNoMarket(t *testing.T) {
	t.Parallel()
	// Encadrement zone with no market reading (only the capper, and its own
	// estimate is empty): the effective rent is the cap alone.
	d := buildDossier(map[string]fakeEntry{
		"encadrement": {data: fakeRentCapper{eurPerM2Cents: 0, capCents: 28_50, capOK: true}},
	})
	got := RentValue(d)
	if got.CapEurPerM2Cents != 28_50 {
		t.Errorf("CapEurPerM2Cents = %d, want 2850", got.CapEurPerM2Cents)
	}
	if got.EffectiveEURPerM2() != 28.50 {
		t.Errorf("EffectiveEURPerM2 = %.2f, want 28.50 (cap alone, no market)", got.EffectiveEURPerM2())
	}
}

func TestRentValue_NoCap(t *testing.T) {
	t.Parallel()
	d := buildDossier(map[string]fakeEntry{
		"carteloyers": {data: fakeRentEstimator{eurPerM2Cents: 18_00, confidence: ConfidenceMedium}},
	})
	got := RentValue(d)
	if got.CapEurPerM2Cents != 0 {
		t.Errorf("CapEurPerM2Cents = %d, want 0 (no encadrement)", got.CapEurPerM2Cents)
	}
	if got.EffectiveEURPerM2() != float64(got.EurPerM2Cents)/100 {
		t.Errorf("EffectiveEURPerM2 = %.2f, want the blend (no cap)", got.EffectiveEURPerM2())
	}
}
