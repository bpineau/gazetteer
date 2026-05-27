// Package carteloyers ports the rental enricher's carte des loyers
// lookup (ANIL / DHUP "carte des loyers") into a standalone gazetteer
// Source. Given a Listing the Source resolves the right typology
// dataset (apartment / house / 1-2 pièces / 3+ pièces) and returns the
// per-m²/month rent reference (loypredm2) with its 80 % prediction
// interval bounds.
//
// The Source is fully offline: every CSV ships embedded under
// `data/`. No HTTP, no geocoder, no upstream API.
package carteloyers

import (
	"fmt"

	"github.com/bpineau/gazetteer/appraisal"
)

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers (a rental wrapper, dashboards) can
// match on them without importing this package's constants.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
	ConfidenceNone   = ""
)

// Typology enumerates the four datasets shipped by the ANIL / DHUP
// carte des loyers. Stored as a string so it round-trips through the
// persisted payload.
type Typology string

const (
	// TypologyApartment is the "all-pieces" apartment dataset
	// (pred-app-mef-dhup.csv).
	TypologyApartment Typology = "apt"

	// TypologyHouse is the maison dataset (pred-mai-mef-dhup.csv).
	TypologyHouse Typology = "house"

	// TypologyApt12 is the 1-2 pièces apartment dataset
	// (pred-app12-mef-dhup.csv).
	TypologyApt12 Typology = "apt_1_2"

	// TypologyApt3 is the 3+ pièces apartment dataset
	// (pred-app3-mef-dhup.csv).
	TypologyApt3 Typology = "apt_3_plus"
)

// Result is the typed payload returned by Source.Query. Mirrors the
// MarketEstimate shape currently persisted by a downstream consumer's rental
// enricher (loyer médian, lo/hi prediction interval, typology,
// confidence, sample size) so the a downstream consumer wrapper can re-serialise
// it 1:1 into its EnrichPayload.Result.
//
// Loyers are in EUR/m²/month "charges comprises" (CC) — the source
// dataset publishes them CC and we keep that convention here. The
// caller decides whether to apply a CC→HC factor (a downstream consumer's wrapper
// applies Config.CCtoHCFactor = 0.90 by default).
//
// Envelope-only fields (schema_version, enricher_version, computed_at,
// input_hash) are NOT part of the gazetteer payload — those are the
// framework's responsibility.
type Result struct {
	// LoyerMedEURPerM2CC is the median rent EUR/m²/month CC for the
	// commune × typology. Zero when no row was found.
	LoyerMedEURPerM2CC float64 `json:"loyer_med_eur_per_m2_cc"`

	// LoyerLowEURPerM2CC and LoyerHighEURPerM2CC are the 80 % prediction
	// interval bounds (lwr_IPm2 / upr_IPm2 from the source CSV). Zero
	// when no row was found.
	LoyerLowEURPerM2CC  float64 `json:"loyer_low_eur_per_m2_cc"`
	LoyerHighEURPerM2CC float64 `json:"loyer_high_eur_per_m2_cc"`

	// Typology records which dataset was consumed ("apt", "apt_1_2",
	// "apt_3_plus", "house") so the audit trail can replay the choice.
	Typology Typology `json:"typology,omitempty"`

	// NbObservations is the commune sample size from the source dataset
	// (nbobs_com). Drives the confidence tier.
	NbObservations int `json:"nb_observations"`

	// Confidence is one of "high" / "medium" / "low" per the
	// PickConfidence rules (commune-fit ≥ 30 obs = high, ≥ 10 = medium,
	// otherwise low; borrowed-neighbour ("maille") fits collapse to
	// low).
	Confidence string `json:"confidence"`

	// Evidence captures reproducibility metadata about the query that
	// produced this Result. Not part of the wire data (json:"-") —
	// populated by Source.Query, consumed in-process by callers that
	// need to log or audit how the answer was derived (e.g.
	// a downstream payload's method params).
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query that
// produced a Result. Consumers that need to log or audit how the answer
// was derived read these fields. Other callers can ignore them.
//
// Sidecar — not part of the wire data. Travels in-process from
// Source.Query to the adapter.
type Evidence struct {
	// INSEE is the 5-digit commune code the Source filtered on. Drawn
	// from Listing.INSEE (mandatory).
	INSEE string `json:"insee"`

	// PropertyType is the canonical property_type from the listing
	// ("apartment" / "house"). The Source rejects every other value
	// with gazetteer.ErrUnsupportedPropertyType.
	PropertyType string `json:"property_type,omitempty"`

	// PredType is the carte des loyers prediction-quality tag
	// ("commune" — fitted against ≥ N observations of the commune
	// itself, or "maille" — borrowed from neighbouring communes). Drives
	// the Confidence classification.
	PredType string `json:"pred_type,omitempty"`

	// Department is the 2-digit (or 3-digit DOM-TOM) department code
	// for the commune. Echoed from the row for traceability.
	Department string `json:"department,omitempty"`

	// FallbackToGeneric is true when the rooms-bucket dataset (apt_1_2
	// or apt_3_plus) had no row for the INSEE and the Source fell back
	// to the generic apartment dataset.
	FallbackToGeneric bool `json:"fallback_to_generic,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// source found no row for the listing — the framework records
// Status == StatusOKEmpty in this case.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.LoyerMedEURPerM2CC <= 0
}

// RentEstimate satisfies appraisal.RentEstimator. Converts the median
// rent EUR/m²/month CC (charges comprises) into the cents convention
// used by the appraisal layer.
//
// CC vs HC: carte des loyers publishes rents charges-comprises ; the
// appraisal layer is unit-agnostic between CC and HC (consumers that
// care about the CC/HC distinction must apply their own conversion
// factor BEFORE passing options to appraisal.RentValue, or branch on
// the Bracket / Method labels). Method records the typology so callers
// can audit which dataset the row came from.
//
// Method follows the "carteloyers_<typology>" convention so downstream
// auditors can tell at a glance which dataset bucket was consumed.
func (r *Result) RentEstimate() appraisal.RentEstimate {
	if r == nil {
		return appraisal.RentEstimate{}
	}
	return appraisal.RentEstimate{
		EurPerM2Cents: int64(r.LoyerMedEURPerM2CC * 100),
		Confidence:    mapCLConfidence(r.Confidence),
		Method:        fmt.Sprintf("carteloyers_%s", nonEmptyTypology(r.Typology)),
	}
}

// mapCLConfidence translates carteloyers's stable confidence strings to
// the appraisal package's coarse enum. Unknown values map to Low so
// callers downstream never panic on a future carteloyers label.
func mapCLConfidence(s string) appraisal.Confidence {
	switch s {
	case ConfidenceHigh:
		return appraisal.ConfidenceHigh
	case ConfidenceMedium:
		return appraisal.ConfidenceMedium
	default:
		return appraisal.ConfidenceLow
	}
}

func nonEmptyTypology(t Typology) string {
	if t == "" {
		return "unknown"
	}
	return string(t)
}
