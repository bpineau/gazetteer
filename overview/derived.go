package overview

import (
	"math"

	"github.com/bpineau/gazetteer/appraisal"
)

// Derived decision fields over a CommuneOverview row. These are the
// standard projections every screening consumer needs and used to
// re-implement (effective price/rent, gross yield, reliability) — they
// live here, next to the data whose semantics they encode, so the rules
// have one home.

// minSmallSampleN is the smallest small-unit sale count below which the
// small-unit median is considered too thin to trust on its own.
const minSmallSampleN = 8

// maxReliableIQRRatio is the P75/P25 dispersion ratio at or above which
// the commune's price distribution is considered bimodal / heterogeneous
// (e.g. a commune mixing a dense center and rural hamlets) — the median
// alone then hides more than it tells.
const maxReliableIQRRatio = 2.0

// EffectivePriceEURM2 returns the price per m² a screening should use:
// the small-unit (T1–T2, 18–55 m²) median when the commune had such
// sales, else the all-unit median (~10 % of communes have no small-unit
// sale in the window; every dvfagg commune has a positive all-unit
// median, so the row stays a usable proxy instead of a zero price).
func (o CommuneOverview) EffectivePriceEURM2() float64 {
	if o.PriceMedianSmallEURM2 > 0 {
		return o.PriceMedianSmallEURM2
	}
	return o.PriceMedianEURM2
}

// EffectiveRentEURM2HC returns the legally chargeable rent per m²/month
// HC: the market rent capped by the encadrement reference when the
// commune is encadrée and the cap is lower. This is the rent number a
// rental-investment decision should use, not the raw market reading. The
// min(market, cap) rule lives once in appraisal.EffectiveRentCents.
func (o CommuneOverview) EffectiveRentEURM2HC() float64 {
	var capCents int64
	if o.RentCapEURM2HC != nil {
		capCents = int64(math.Round(*o.RentCapEURM2HC * 100))
	}
	return float64(appraisal.EffectiveRentCents(int64(math.Round(o.RentMarketEURM2HC*100)), capCents)) / 100
}

// GrossYieldPct returns the gross rental yield in percent
// (EffectiveRent × 12 / EffectivePrice × 100), or 0 when the row lacks
// a usable price.
func (o CommuneOverview) GrossYieldPct() float64 {
	price := o.EffectivePriceEURM2()
	if price <= 0 {
		return 0
	}
	return o.EffectiveRentEURM2HC() * 12 / price * 100
}

// PriceReliable reports whether the row's price medians can be taken at
// face value: at least minSmallSampleN small-unit sales AND a P75/P25
// dispersion below maxReliableIQRRatio. A false here flags a thin or
// bimodal market — screeners should surface the row with a caveat (or
// drop it) rather than rank on its median.
func (o CommuneOverview) PriceReliable() bool {
	if o.PriceNSmall < minSmallSampleN {
		return false
	}
	if o.PriceP25EURM2 > 0 && o.PriceP75EURM2/o.PriceP25EURM2 >= maxReliableIQRRatio {
		return false
	}
	return true
}
