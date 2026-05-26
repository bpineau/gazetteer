// Package encadrement ports the rental enricher's zone-encadrement
// lookup (Paris, Plaine Commune, Lyon / Villeurbanne) into a standalone
// gazetteer Source. Given a Listing the Source resolves the
// arrondissement / IRIS / zone for the property and returns the legal
// loyer de référence (and majoré cap) per m² month HC.
//
// The Source is fully offline: embedded JSON tables ship under `data/`.
package encadrement

import (
	"fmt"

	"github.com/bpineau/gazetteer/appraisal"
)

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers (encheridor's rental wrapper, dashboards) can
// match on them without importing this package's constants.
const (
	ConfidenceMedium = "medium"
	ConfidenceNone   = ""
)

// ZoneSource enumerates the three published zones encadrées this Source
// covers. Surfaced verbatim in Result.ZoneSource so the UI can label
// the section correctly.
const (
	ZoneSourceParis            = "paris"
	ZoneSourcePlaineCommune    = "plaine_commune"
	ZoneSourceLyonVilleurbanne = "lyon_villeurbanne"
)

// Result is the typed payload returned by Source.Query. Mirrors the
// CeilingEstimate shape currently persisted by encheridor's rental
// enricher (loyer de référence + majoré + zone label) so the wrapper
// can re-serialise it 1:1 into its EnrichPayload.Result.
//
// Envelope-only fields are NOT part of the gazetteer payload — those
// are the framework's responsibility.
type Result struct {
	// LoyerRefMajEURPerM2HC is the loyer de référence majoré (the legal
	// max) in EUR/m²/month HC. Zero when the property sits outside the
	// shipped zones.
	LoyerRefMajEURPerM2HC float64 `json:"loyer_ref_maj_eur_per_m2_hc"`

	// LoyerRefEURPerM2HC is the loyer de référence (the published
	// reference, ~20 % below the majoré).
	LoyerRefEURPerM2HC float64 `json:"loyer_ref_eur_per_m2_hc"`

	// Zone is the human-readable zone label ("Paris 11e", "Lyon 3e",
	// "Villeurbanne"). Empty when no match.
	Zone string `json:"zone,omitempty"`

	// ZoneSource is one of "paris" | "plaine_commune" |
	// "lyon_villeurbanne". Empty when no match.
	ZoneSource string `json:"zone_source,omitempty"`

	// Confidence is "medium" on a match (the grille gives a single value
	// with no per-cell sample size, so the operator-facing copy flags
	// the "approx époque" caveat). ConfidenceNone otherwise.
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
	// Zip is the 5-digit FR postal code the Source consumed when
	// resolving the Paris arrondissement. Empty outside Paris.
	Zip string `json:"zip,omitempty"`

	// INSEE is the 5-digit commune code the Source filtered on (Lyon
	// arrondissements + Villeurbanne).
	INSEE string `json:"insee,omitempty"`

	// Arrondissement is the 2-digit Paris arrondissement key
	// ("01" .. "20"). Empty outside Paris.
	Arrondissement string `json:"arrondissement,omitempty"`

	// Piece is the rooms bucket the Source picked (1..4). Open-ended
	// cells ("4 et plus") match any piece ≥ their stored Piece.
	Piece int `json:"piece,omitempty"`

	// NbCellsMatched is the number of grille cells that matched the
	// (zone, piece, non-meublé, non-maison) filter. Drives the
	// median collapse.
	NbCellsMatched int `json:"nb_cells_matched,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// source found no zone-encadrement cell for the listing — the framework
// records Status == StatusOKEmpty in this case.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.LoyerRefMajEURPerM2HC <= 0
}

// RentEstimate satisfies appraisal.RentEstimator. The encadrement
// source is a LEGAL CAP, not a market estimate — the contribution
// surfaces:
//
//   - EurPerM2Cents : the loyer de référence (the published median
//     equivalent, ~20 % below the majoré cap) in centimes/m²/month HC.
//     We use the reference rather than the majoré because callers
//     consuming the consolidated rent want a representative reading,
//     not the upper bound; the majoré is what the lease may legally
//     hit, the reference is what an "ordinary" rent looks like.
//
//   - Bracket : "encadrement_<zone_source>_<zone>" so the appraisal
//     layer can surface the regulated label on the consolidated
//     output. Empty when the zone didn't match.
//
//   - Method : "encadrement_<zone_source>_p<piece>" so auditors can
//     replay the (zone, rooms) cell that produced the row.
//
// HC vs CC: encadrement publishes hors-charges by construction (the
// grille is HC). Consumers that need a CC reading must apply the
// CC/HC factor; appraisal.RentValue is unit-agnostic and trusts the
// caller to feed comparable readings.
func (r *Result) RentEstimate() appraisal.RentEstimate {
	if r == nil {
		return appraisal.RentEstimate{}
	}
	return appraisal.RentEstimate{
		EurPerM2Cents: int64(r.LoyerRefEURPerM2HC * 100),
		Confidence:    mapEncConfidence(r.Confidence),
		Bracket:       bracketFor(r),
		Method: fmt.Sprintf("encadrement_%s_p%d",
			nonEmptyZoneSource(r.ZoneSource),
			r.Evidence.Piece),
	}
}

// mapEncConfidence translates encadrement's stable confidence strings to
// the appraisal package's coarse enum. Unknown values map to Low so
// callers downstream never panic on a future encadrement label.
func mapEncConfidence(s string) appraisal.Confidence {
	switch s {
	case ConfidenceMedium:
		return appraisal.ConfidenceMedium
	default:
		return appraisal.ConfidenceLow
	}
}

func nonEmptyZoneSource(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// bracketFor builds the regulated-zone identifier exposed via
// RentEstimate.Bracket. Empty when neither ZoneSource nor Zone is set
// (the result is empty / IsEmpty would also be true).
func bracketFor(r *Result) string {
	if r == nil || (r.ZoneSource == "" && r.Zone == "") {
		return ""
	}
	if r.ZoneSource == "" {
		return "encadrement_" + r.Zone
	}
	if r.Zone == "" {
		return "encadrement_" + r.ZoneSource
	}
	return "encadrement_" + r.ZoneSource + "_" + r.Zone
}
