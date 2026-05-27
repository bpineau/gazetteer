package delinquance

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers can match on them without importing this
// package's constants.
const (
	ConfidenceHigh = "high"
	ConfidenceNone = ""
)

// RiskFlag is one of "low" | "medium" | "high" | "unknown". A coarse,
// peer-relative bucket derived from the burglary + vandalism +
// no-violence-theft headline indicators. Informative only — never
// folded into a score by this Source.
type RiskFlag string

const (
	RiskUnknown RiskFlag = "unknown"
	RiskLow     RiskFlag = "low"
	RiskMedium  RiskFlag = "medium"
	RiskHigh    RiskFlag = "high"
)

// Result is the typed payload returned by Source.Query.
//
// The rate map is keyed by the SSMSI indicator's short English handle —
// see loader.LABEL for the upstream French → handle mapping. Every
// rate is expressed in events per 1 000 inhabitants per year (or per
// 1 000 logements for the burglary indicator, which is the SSMSI
// convention).
type Result struct {
	// Rates is the indicator → events-per-1000 rate map for the
	// reference year. Empty when the commune is missing from the
	// dataset.
	Rates map[string]float64 `json:"rates,omitempty"`

	// Population is the INSEE-published resident population used as
	// the rate denominator (zero when the commune is missing).
	Population int `json:"population,omitempty"`

	// Flag is the coarse, peer-relative risk bucket. RiskUnknown when
	// the commune is missing from the dataset.
	Flag RiskFlag `json:"flag,omitempty"`

	// Confidence is "high" when the commune was found in the dataset,
	// ConfidenceNone otherwise.
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

	// DataYear is the SSMSI reference year for the embedded extract
	// (e.g. 2024).
	DataYear int `json:"data_year,omitempty"`

	// Unit is the rate denominator ("per_thousand"). Stable scalar
	// the renderer can echo.
	Unit string `json:"unit,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// commune was missing from the dataset — the framework records
// Status == StatusOKEmpty in this case.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return len(r.Rates) == 0
}

// classifyRisk applies a simple peer-relative classifier on the
// headline burglary + vandalism + no-violence-theft rates. Thresholds
// are calibrated against the 2024 national distribution:
//
//	low    : burglary <= 2.5 ‰ AND theft <= 8 ‰ AND vandalism <= 5 ‰
//	high   : burglary >= 6 ‰   OR  theft >= 25 ‰ OR vandalism >= 12 ‰
//	medium : everything in between
func classifyRisk(rates map[string]float64) RiskFlag {
	if len(rates) == 0 {
		return RiskUnknown
	}
	b := rates["burglary"]
	t := rates["theft_no_violence"]
	v := rates["vandalism"]
	if b >= 6 || t >= 25 || v >= 12 {
		return RiskHigh
	}
	if b <= 2.5 && t <= 8 && v <= 5 {
		return RiskLow
	}
	return RiskMedium
}
