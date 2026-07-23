// Package encadrement ports the rental enricher's zone-encadrement
// lookup (Paris, Plaine Commune, Est Ensemble, Lyon / Villeurbanne) into
// a standalone gazetteer Source. Given a Listing the Source resolves the
// arrondissement / zone / IRIS for the property and returns the legal
// loyer de référence (and majoré cap) per m² month HC.
//
// The Source is fully offline: embedded JSON tables (barèmes) and
// GeoJSON-derived zonage geometry ship under `data/`.
package encadrement

import (
	"fmt"
	"math"

	"github.com/bpineau/gazetteer/appraisal"
)

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers (appraisers, dashboards) can match on them
// without importing this package's constants.
const (
	ConfidenceMedium = "medium"
	// ConfidenceLow flags a resolved-but-ambiguous reading: a multi-zone EPT
	// commune (Saint-Denis, Montreuil) queried without coordinates, collapsed
	// across all its zones.
	ConfidenceLow  = "low"
	ConfidenceNone = ""
)

// ZoneSource enumerates the published zones encadrées this Source covers.
// Surfaced verbatim in Result.ZoneSource so the UI can label the section
// correctly.
const (
	ZoneSourceParis            = "paris"
	ZoneSourcePlaineCommune    = "plaine_commune"
	ZoneSourceEstEnsemble      = "est_ensemble"
	ZoneSourceLyonVilleurbanne = "lyon_villeurbanne"
)

// Result is the typed payload returned by Source.Query. Exposes the
// loyer de référence + majoré + zone label trio.
//
// Envelope-only fields are NOT part of this payload — those are the
// framework's responsibility (see gazetteer.Result).
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

	// ZoneSource is one of "paris" | "plaine_commune" | "est_ensemble" |
	// "lyon_villeurbanne". Empty when no match.
	ZoneSource string `json:"zone_source,omitempty"`

	// Confidence is "medium" on a clean match — the grille gives a single
	// value with no per-cell sample size, so the operator-facing copy flags
	// the "approx époque" caveat — and "low" on a resolved-but-ambiguous
	// match (a multi-zone EPT commune queried without coordinates, collapsed
	// across its zones; see ConfidenceLow). ConfidenceNone otherwise.
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

	// ZoneID is the resolved zone identifier inside the source — the Plaine
	// Commune / Est Ensemble zone number (e.g. "311"). For the ambiguous
	// commune-fallback path it is the "+"-joined set of the commune's zones
	// (e.g. "311+312"). Empty outside the Seine-Saint-Denis EPTs.
	ZoneID string `json:"zone_id,omitempty"`
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
		EurPerM2Cents: int64(math.Round(r.LoyerRefEURPerM2HC * 100)),
		Confidence:    mapEncConfidence(r.Confidence),
		Bracket:       bracketFor(r),
		Method: fmt.Sprintf("encadrement_%s_p%d",
			nonEmptyZoneSource(r.ZoneSource),
			r.Evidence.Piece),
	}
}

// RentCap satisfies appraisal.RentCapper: it contributes the loyer de référence
// majoré (the legal ceiling, HC €/m²/month → centimes) so consumers can clamp a
// market rent to the legal maximum without re-deriving the cap. ok is false
// outside an encadrement zone (no published majoré).
func (r *Result) RentCap() (int64, bool) {
	if r == nil || r.LoyerRefMajEURPerM2HC <= 0 {
		return 0, false
	}
	return int64(math.Round(r.LoyerRefMajEURPerM2HC * 100)), true
}

// mapEncConfidence translates encadrement's stable confidence strings to
// the appraisal package's coarse enum. Deliberately NOT
// appraisal.ParseConfidence: encadrement never emits "high", so any
// unknown label — including a future "high" — collapses to Low (the
// conservative floor pinned by TestResult_RentEstimateConfidenceMapping).
func mapEncConfidence(s string) appraisal.Confidence {
	switch s {
	case ConfidenceMedium:
		return appraisal.ConfidenceMedium
	case ConfidenceLow:
		return appraisal.ConfidenceLow
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
//
// It keys on the stable Evidence.ZoneID when present (the Seine-Saint-Denis
// EPTs set it — e.g. "encadrement_plaine_commune_311"), falling back to the
// human Zone label for Paris / Lyon, so the Bracket stays a grep-stable token
// rather than a free-text label.
func bracketFor(r *Result) string {
	if r == nil || (r.ZoneSource == "" && r.Zone == "") {
		return ""
	}
	zone := r.Zone
	if r.Evidence.ZoneID != "" {
		zone = r.Evidence.ZoneID
	}
	if r.ZoneSource == "" {
		return "encadrement_" + zone
	}
	if zone == "" {
		return "encadrement_" + r.ZoneSource
	}
	return "encadrement_" + r.ZoneSource + "_" + zone
}
