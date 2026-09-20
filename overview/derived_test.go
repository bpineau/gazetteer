package overview

import (
	"math"
	"testing"
)

func fp(v float64) *float64 { return &v }

func TestEffectivePriceEURM2(t *testing.T) {
	t.Parallel()
	// The small-unit median wins once it rests on enough sales; see
	// TestEffectivePriceEURM2_ThinSmallSample for the floor itself.
	if got := (CommuneOverview{PriceMedianSmallEURM2: 4200, PriceMedianEURM2: 3800, PriceNSmall: 40}).EffectivePriceEURM2(); got != 4200 {
		t.Errorf("small median preferred: got %v", got)
	}
	if got := (CommuneOverview{PriceMedianEURM2: 3800}).EffectivePriceEURM2(); got != 3800 {
		t.Errorf("fallback to all-unit median: got %v", got)
	}
}

func TestEffectiveRentEURM2HC(t *testing.T) {
	t.Parallel()
	if got := (CommuneOverview{RentMarketEURM2HC: 20}).EffectiveRentEURM2HC(); got != 20 {
		t.Errorf("no cap: got %v", got)
	}
	if got := (CommuneOverview{RentMarketEURM2HC: 20, RentCapEURM2HC: fp(16.5), Encadree: true}).EffectiveRentEURM2HC(); got != 16.5 {
		t.Errorf("cap below market wins: got %v", got)
	}
	if got := (CommuneOverview{RentMarketEURM2HC: 14, RentCapEURM2HC: fp(16.5), Encadree: true}).EffectiveRentEURM2HC(); got != 14 {
		t.Errorf("market below cap wins: got %v", got)
	}
}

func TestGrossYieldPct(t *testing.T) {
	t.Parallel()
	o := CommuneOverview{PriceMedianSmallEURM2: 3000, PriceNSmall: 40, RentMarketEURM2HC: 15}
	want := 15.0 * 12 / 3000 * 100 // 6 %
	if got := o.GrossYieldPct(); math.Abs(got-want) > 1e-9 {
		t.Errorf("yield = %v, want %v", got, want)
	}
	if got := (CommuneOverview{RentMarketEURM2HC: 15}).GrossYieldPct(); got != 0 {
		t.Errorf("no price ⇒ 0, got %v", got)
	}
}

func TestPriceReliable(t *testing.T) {
	t.Parallel()
	ok := CommuneOverview{PriceNSmall: 12, PriceP25EURM2: 3000, PriceP75EURM2: 4500}
	if !ok.PriceReliable() {
		t.Error("healthy row flagged unreliable")
	}
	thin := CommuneOverview{PriceNSmall: 7, PriceP25EURM2: 3000, PriceP75EURM2: 4500}
	if thin.PriceReliable() {
		t.Error("thin sample (<8) must be unreliable")
	}
	bimodal := CommuneOverview{PriceNSmall: 30, PriceP25EURM2: 2000, PriceP75EURM2: 4000}
	if bimodal.PriceReliable() {
		t.Error("P75/P25 >= 2.0 must be unreliable")
	}
}

// TestEffectivePriceEURM2_ThinSmallSample pins the sample floor: the
// small-unit median only speaks for the commune once it rests on enough
// sales. Values are Longuyon's (INSEE 54322) in the embedded aggregate.
func TestEffectivePriceEURM2_ThinSmallSample(t *testing.T) {
	t.Parallel()
	thin := CommuneOverview{
		PriceMedianEURM2:      845.14,
		PriceP25EURM2:         682.93,
		PriceP75EURM2:         1066.67,
		PriceN:                89,
		PriceMedianSmallEURM2: 3425,
		PriceNSmall:           1,
	}
	if got := thin.EffectivePriceEURM2(); got != 845.14 {
		t.Errorf("EffectivePriceEURM2 = %.2f, want the 89-sale median 845.14 (one small-unit sale cannot set a commune's price)", got)
	}
	if thin.PriceReliable() {
		t.Error("PriceReliable() = true, want false on a 1-sale small band")
	}

	solid := thin
	solid.PriceNSmall = minSmallSampleN
	if got := solid.EffectivePriceEURM2(); got != 3425 {
		t.Errorf("EffectivePriceEURM2 = %.2f, want the small-unit median 3425 once the sample clears the floor", got)
	}

	// No small-unit sale at all still falls back rather than reading zero.
	none := thin
	none.PriceMedianSmallEURM2, none.PriceNSmall = 0, 0
	if got := none.EffectivePriceEURM2(); got != 845.14 {
		t.Errorf("EffectivePriceEURM2 = %.2f, want the all-unit median 845.14", got)
	}
}
