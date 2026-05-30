// Package filoiris is a gazetteer.Source that returns INSEE Filosofi
// income indicators at the IRIS (sub-commune) level, keyed by the
// listing's resolved IRIS code. Where the commune-level `filosofi`
// source answers "how wealthy is this town", filoiris answers "how
// wealthy is this *neighbourhood*" — the distinction that matters most
// in dense, socially-mixed communes (Paris, petite couronne), where a
// single commune spans a 2x spread in median income across its IRIS.
//
// The Source is fully offline: the IRIS Filosofi table ships embedded
// (gzipped JSON) under `data/`. It only answers for listings whose
// Listing.IRIS is populated (cf. the `iris` source / the BAN
// normalizer's IRIS resolver) and that fall in an IRIS INSEE publishes
// (communes ≥ 5000 inhabitants, secret statistique permitting).
package filoiris

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers can match on them without importing this
// package's constants.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceNone   = ""
)

// RiskFlag is one of "low" | "medium" | "high" | "unknown" — a coarse,
// opinionated income-risk bucket derived from the IRIS median revenu
// disponible + the taux de pauvreté. Informative only; never folded
// into a score by this Source.
type RiskFlag string

const (
	RiskUnknown RiskFlag = "unknown"
	RiskLow     RiskFlag = "low"
	RiskMedium  RiskFlag = "medium"
	RiskHigh    RiskFlag = "high"
)

// Result is the typed payload returned by Source.Query.
type Result struct {
	// MedianEUR is the IRIS's revenu disponible médian par UC (€/an).
	// Zero when the IRIS was not found or its value is suppressed
	// (ns / nd in the upstream).
	MedianEUR int `json:"median_eur"`

	// PovertyRatePct is the IRIS's taux de pauvreté au seuil de 60 %
	// (DISP_TP60, %). Zero when not published.
	PovertyRatePct float64 `json:"poverty_rate_pct,omitempty"`

	// Gini is the IRIS's Gini index of disposable income (0..1).
	// Higher = more unequal — a "socially mixed" signal even when the
	// median looks comfortable. Zero when not published.
	Gini float64 `json:"gini,omitempty"`

	// Flag is the coarse income-risk bucket. RiskUnknown when the IRIS
	// is missing from the dataset.
	Flag RiskFlag `json:"flag,omitempty"`

	// Confidence is "high" when both median + poverty rate are
	// populated, "medium" when only the median was found,
	// ConfidenceNone when the IRIS is missing.
	Confidence string `json:"confidence"`

	// Evidence captures reproducibility metadata. Sidecar — not wire
	// data (json:"-"); populated by Source.Query.
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query that
// produced a Result.
type Evidence struct {
	// IRIS is the 9-digit IRIS code the Source filtered on (from
	// Listing.IRIS).
	IRIS string `json:"iris"`

	// DataYear is the Filosofi reference year (e.g. 2021).
	DataYear int `json:"data_year,omitempty"`

	// NationalMedianEUR is the median of IRIS medians — a sanity scalar
	// downstream renderers can compare against.
	NationalMedianEUR int `json:"national_median_eur,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter: true when the IRIS was not
// found / has no published median. The framework then records
// StatusOKEmpty.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.MedianEUR <= 0
}
