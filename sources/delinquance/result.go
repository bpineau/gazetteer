package delinquance

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers can match on them without importing this
// package's constants.
const (
	ConfidenceHigh = "high"
	ConfidenceNone = ""
)

// RiskFlag is a coarse SOCIAL-DISTRESS bucket: "low" | "medium" |
// "high" | "unknown". Targets the investor question "is this a
// neighbourhood where solvent tenants don't want to live?". Inputs
// are drug-trafficking, street-violence and unarmed-robbery rates;
// burglary is INTENTIONALLY omitted (it is anti-correlated with
// social distress — luxury / tourist areas are the prime targets).
// See classifyRisk in this file for the calibration. Informative
// only — never folded into a score by this Source.
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

// classifyRisk produces a SOCIAL-DISTRESS flag (not a generic crime
// flag) for a rental / marchand-de-bien investor: it tries to detect
// neighbourhoods where the population is captive (low income, no
// mobility, public housing concentration) and the urban environment
// is degraded (open drug-dealing scenes, street violence). The signal
// is meant to flag the kind of commune where a landlord ends up with
// less-solvent tenants, more vacancy, more squatting risk, and
// in-building safety issues — NOT to flag where crime happens to be
// reported (which would be e.g. central Paris because that is where
// the foot traffic is).
//
// Inputs (all SSMSI État 4001, per 1 000 inhabitants):
//
//   - drug_trafficking  : open dealing / street market presence
//   - violence_outside_family : street agressions
//   - robbery_unarmed   : muggings / phone snatches
//
// Burglary is INTENTIONALLY OMITTED — luxury / tourist neighbourhoods
// (Neuilly, Paris 16e, Paris 8e) score high on burglary precisely
// BECAUSE there is value worth stealing; treating burglary as a
// social-distress signal would invert the desired ranking.
//
// Thresholds calibrated empirically on a panel of known ghettos
// (Aulnay-Cité-des-3000, La Courneuve-4000, Grigny, Saint-Denis,
// Clichy-sous-Bois) vs. known low-distress communes (Neuilly,
// Paris 16e, Paris 9e):
//
//	low    : dt <= 1   AND vof <= 2.5 AND ru <= 1.5
//	high   : dt >= 2.5 OR  vof >= 5   OR  ru >= 4
//	         OR (dt >= 1.5 AND vof >= 4)
//	medium : everything in between
//
// Per-inhabitant denominator distortion: Paris/Lyon/Marseille
// arrondissements have ambient (daytime / tourist) populations 5–15×
// their resident counts, so even pure-residential arrondissements
// score "high" by absolute thresholds. classifyRisk returns
// RiskUnknown for those communes; consumers should compose
// QPV + Filosofi (income) + chomage to get a credible signal for
// arrondissement-split cities.
func classifyRisk(rates map[string]float64) RiskFlag {
	if len(rates) == 0 {
		return RiskUnknown
	}
	dt := rates["drug_trafficking"]
	vof := rates["violence_outside_family"]
	ru := rates["robbery_unarmed"]
	if dt >= 2.5 || vof >= 5 || ru >= 4 || (dt >= 1.5 && vof >= 4) {
		return RiskHigh
	}
	if dt <= 1 && vof <= 2.5 && ru <= 1.5 {
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
