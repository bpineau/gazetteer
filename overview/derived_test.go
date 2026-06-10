package overview

import (
	"math"
	"testing"
)

func fp(v float64) *float64 { return &v }

func TestEffectivePriceEURM2(t *testing.T) {
	t.Parallel()
	if got := (CommuneOverview{PriceMedianSmallEURM2: 4200, PriceMedianEURM2: 3800}).EffectivePriceEURM2(); got != 4200 {
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
	o := CommuneOverview{PriceMedianSmallEURM2: 3000, RentMarketEURM2HC: 15}
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
