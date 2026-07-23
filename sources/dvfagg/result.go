package dvfagg

import (
	"math"

	"github.com/bpineau/gazetteer/appraisal"
)

// Name is the canonical Source identifier (Dossier key + registry key).
const Name = "dvfagg"

// Version bumps when the Source's internal logic changes (gates datadir reuse).
//
// v2 adds PriceEstimate (appraisal.PriceEstimator) so the commune median can
// satisfy appraisal.PricePerM2 offline, and moves the refresh window to
// 2023-2025.
const Version = 2

// Result is the per-commune DVF price aggregate, in EUR/m², 3-year window.
// Built at refresh time from single-lot apartment sales; see transform.go.
type Result struct {
	// PriceMedianEURM2 / P25 / P75 are the dispersion of €/m² over all
	// single-lot apartment sales of the commune (P25≪P50≪P75 ⇒ bimodal).
	PriceMedianEURM2 float64 `json:"price_median_eur_m2"`
	PriceP25EURM2    float64 `json:"price_p25_eur_m2"`
	PriceP75EURM2    float64 `json:"price_p75_eur_m2"`
	// PriceMedianSmallEURM2 is the median €/m² restricted to 18–55 m²
	// (the figure to pair with a T1–T2 rent). Zero when n_small == 0.
	PriceMedianSmallEURM2 float64 `json:"price_median_small_eur_m2"`
	// N / NSmall are sample sizes (reliability).
	N      int    `json:"n"`
	NSmall int    `json:"n_small"`
	Dept   string `json:"department"`

	// Evidence is the reproducibility sidecar (json:"-"), per the uniform
	// Source contract. It records which commune INSEE produced the aggregate.
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the lookup.
type Evidence struct {
	// INSEE is the commune code that was looked up to produce this Result.
	INSEE string `json:"insee,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter: no sales ⇒ empty.
func (r *Result) IsEmpty() bool { return r == nil || r.N == 0 }

// Sample-size and dispersion tiers for the commune-median confidence. They
// mirror overview.PriceReliable: a thin or bimodal (P75/P25 ≥ 2) market is not
// trustworthy at face value.
const (
	priceHighN        = 50
	priceMediumN      = 15
	priceMaxIQRRatio  = 2.0
	priceEstimateName = "dvfagg_commune_median"
)

// PriceEstimate satisfies appraisal.PriceEstimator: it contributes the
// commune-wide median €/m² (over single-lot apartment sales, 3-year window) to
// appraisal.PricePerM2. This lets the synthesis reach its MinSources=2 floor —
// and thus a Medium/High price_confidence — from embedded data alone, pairing
// with the live dvf source without a second network reading.
//
// Confidence tracks the sample size and dispersion: a large, tight commune
// (N≥50, P75/P25<2) is High; a solid one (N≥15) Medium; a thin or bimodal
// market Low. An empty aggregate returns a zero-value Low estimate — callers
// gate on IsEmpty first.
func (r *Result) PriceEstimate() appraisal.PriceEstimate {
	if r == nil {
		return appraisal.PriceEstimate{Method: priceEstimateName}
	}
	return appraisal.PriceEstimate{
		EurPerM2Cents: int64(math.Round(r.PriceMedianEURM2 * 100)),
		Confidence:    r.priceConfidence(),
		SampleSize:    r.N,
		Method:        priceEstimateName,
	}
}

// priceConfidence maps sample size + dispersion to a confidence tier.
func (r *Result) priceConfidence() appraisal.Confidence {
	bimodal := r.PriceP25EURM2 > 0 && r.PriceP75EURM2/r.PriceP25EURM2 >= priceMaxIQRRatio
	switch {
	case r.N >= priceHighN && !bimodal:
		return appraisal.ConfidenceHigh
	case r.N >= priceMediumN:
		return appraisal.ConfidenceMedium
	default:
		return appraisal.ConfidenceLow
	}
}
