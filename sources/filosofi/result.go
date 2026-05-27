// Package filosofi ports the rental enricher's INSEE Filosofi commune
// lookup into a standalone gazetteer Source. Given a Listing the
// Source resolves the commune's INSEE and returns the revenu
// disponible médian + part de minima sociaux (proxy for the local
// poverty rate), plus an opinionated risk flag.
//
// The Source is fully offline: the Filosofi JSON ships embedded under
// `data/`.
package filosofi

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers can match on them without importing this
// package's constants.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceNone   = ""
)

// RiskFlag is one of "low" | "medium" | "high" | "unknown". A coarse,
// opinionated bucket derived from the Filosofi median revenu
// disponible + the part de minima sociaux. Informative only — never
// folded into a score by this Source.
type RiskFlag string

const (
	RiskUnknown RiskFlag = "unknown"
	RiskLow     RiskFlag = "low"
	RiskMedium  RiskFlag = "medium"
	RiskHigh    RiskFlag = "high"
)

// Result is the typed payload returned by Source.Query. Exposes the
// commune's income + minima-sociaux indicators plus the peer-relative
// RiskFlag.
type Result struct {
	// MedianEUR is the commune's revenu disponible médian par UC
	// (€/an). Zero when the commune was not found (secret statistique
	// drops on small communes).
	MedianEUR int `json:"median_eur"`

	// MinimaPct is the part des minima sociaux dans le revenu
	// disponible (%) — proxy for the local poverty rate. Zero when the
	// dataset did not publish the value for the commune (small / DOM-TOM /
	// Corsica).
	MinimaPct float64 `json:"minima_pct,omitempty"`

	// Flag is the coarse risk bucket the UI surfaces. RiskUnknown when
	// the commune is missing from Filosofi.
	Flag RiskFlag `json:"flag,omitempty"`

	// Confidence is "high" when both median + minima are populated,
	// "medium" when only the median was found, ConfidenceNone when the
	// commune is missing.
	Confidence string `json:"confidence"`

	// Evidence captures reproducibility metadata about the query that
	// produced this Result. Not part of the wire data (json:"-") —
	// populated by Source.Query, consumed in-process by callers that
	// need to log or audit how the answer was derived.
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query that
// produced a Result.
//
// Sidecar — not part of the wire data. Travels in-process from
// Source.Query to the adapter.
type Evidence struct {
	// INSEE is the 5-digit commune code the Source filtered on. Drawn
	// from Listing.INSEE (mandatory).
	INSEE string `json:"insee"`

	// DataYear is the Filosofi reference year (from the embedded
	// manifest, e.g. 2021).
	DataYear int `json:"data_year,omitempty"`

	// NationalMedianEUR is the national median revenu disponible per
	// UC (€/an). Useful sanity scalar for downstream renderers.
	NationalMedianEUR int `json:"national_median_eur,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// commune was not found in the Filosofi dataset — the framework
// records Status == StatusOKEmpty in this case.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.MedianEUR <= 0
}
