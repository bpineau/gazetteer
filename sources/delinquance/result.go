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

	// Flag is the coarse, peer-relative risk bucket derived from the
	// burglary indicator (per 1 000 logements, robust to ambient
	// population). RiskUnknown when the commune is missing from the
	// dataset.
	Flag RiskFlag `json:"flag,omitempty"`

	// RatesPerInhabitantInflated is true when the commune's
	// per-inhabitant rates (theft_no_violence, vandalism, drug_use,
	// drug_trafficking, fraud, …) are likely inflated by the ambient
	// (daytime / tourist) population being much larger than the
	// resident population SSMSI uses as the denominator. Renderers
	// SHOULD suppress or footnote those rates when this flag is true:
	// raw "Paris 1er theft = 325 ‰" is statistically meaningless for
	// an investor's risk assessment.
	//
	// `Flag` itself is NOT affected — it only uses burglary, which is
	// per-logement and thus robust to this distortion.
	RatesPerInhabitantInflated bool `json:"rates_per_inhabitant_inflated,omitempty"`

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

// classifyRisk applies a classifier on the burglary indicator
// only. Thresholds are calibrated against the 2024 national
// distribution:
//
//	low    : burglary <= 2.5 ‰
//	high   : burglary >= 6 ‰
//	medium : everything in between
//
// Burglary is the only headline indicator the SSMSI expresses per
// 1 000 *logements* (not per 1 000 inhabitants), which makes it
// robust to the denominator distortion that plagues per-inhabitant
// rates in touristic / business-district arrondissements (Paris,
// Lyon, Marseille). Earlier versions of this function folded
// theft_no_violence and vandalism into the classification — but
// those are per-inhabitant rates, and Paris 1er triple-tripped the
// high threshold (theft 325 ‰, vandalism 24 ‰) despite a real
// resident-perspective risk closer to other Paris arrondissements.
// Consumers that need the broader picture should read the typed
// Rates map directly and apply their own thresholds, ideally with
// the RatesPerInhabitantInflated caveat on the Result in mind.
func classifyRisk(rates map[string]float64) RiskFlag {
	if len(rates) == 0 {
		return RiskUnknown
	}
	b := rates["burglary"]
	if b >= 6 {
		return RiskHigh
	}
	if b <= 2.5 {
		return RiskLow
	}
	return RiskMedium
}

// hasInflatedPerInhabitantRates reports whether the commune's
// per-inhabitant rates (theft_no_violence, vandalism, drug_use, …)
// are likely inflated by the ambient (daytime / tourist) population
// being much larger than the resident population SSMSI uses as the
// denominator.
//
// The current heuristic covers the three biggest offenders —
// arrondissement-split Paris (75101–75120), Lyon (69381–69389) and
// Marseille (13201–13216). Future versions may add data-driven
// detection (population density, hotel capacity, employment
// gravity) for the long tail.
func hasInflatedPerInhabitantRates(insee string) bool {
	if len(insee) != 5 {
		return false
	}
	switch {
	case insee >= "75101" && insee <= "75120":
		return true
	case insee >= "69381" && insee <= "69389":
		return true
	case insee >= "13201" && insee <= "13216":
		return true
	}
	return false
}
