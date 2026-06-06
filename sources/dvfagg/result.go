package dvfagg

// Name is the canonical Source identifier (Dossier key + registry key).
const Name = "dvfagg"

// Version bumps when the Source's internal logic changes (gates datadir reuse).
const Version = 1

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
}

// IsEmpty satisfies gazetteer.EmptyReporter: no sales ⇒ empty.
func (r *Result) IsEmpty() bool { return r == nil || r.N == 0 }
