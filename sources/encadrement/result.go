// Package encadrement ports the rental enricher's zone-encadrement
// lookup (Paris, Plaine Commune, Est Ensemble, Lyon / Villeurbanne) into
// a standalone gazetteer Source. Given a Listing the Source resolves the
// arrondissement / zone / IRIS for the property and returns the legal
// loyer de référence (and majoré cap) per m² month HC, for an unfurnished
// and for a furnished letting.
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
	// commune (Saint-Denis, Montreuil) queried without coordinates, or a Paris
	// or Lyon arrondissement queried without an IRIS whose zones carry
	// different caps, collapsed across all its zones.
	ConfidenceLow  = "low"
	ConfidenceNone = ""
)

// Regulation values returned in Result.Regulation. Stable strings.
const (
	// RegulationCapped: the address lies in a shipped zone and the Result
	// carries its cap.
	RegulationCapped = "capped"
	// RegulationNotCovered: the address lies in a decreed rent-cap territory,
	// so its rent is capped, but the Source ships no grille for it (or no
	// cell of the shipped grille matched). The caps are zero and
	// Confidence is ConfidenceNone: the figure is unknown, not absent.
	RegulationNotCovered = "regulated_not_covered"
	// RegulationPartialCommune: the decree covers only part of the commune
	// (a set of IRIS) and the Source does not resolve which part, so the
	// address may or may not be capped.
	RegulationPartialCommune = "partial_commune"
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

	// LoyerRefMajMeubleEURPerM2HC is the loyer de référence majoré of a
	// FURNISHED letting (location meublée) on the same cells, EUR/m²/month HC:
	// the legal max of a meublé lease, which is what caps a furnished (LMNP)
	// rent. LoyerRefMajEURPerM2HC caps an unfurnished lease only and sits below
	// it on every published cell (Paris 2026 adds 2.1 to 5.3 EUR/m²/month to the
	// référence depending on the secteur and cell, so 1.2 times that to the
	// majoré). Collapsed across the same zone, pièces and époque selection as
	// the unfurnished pair, by median. Zero when the property sits outside the
	// shipped zones or the zone publishes no meublé cell for the selection.
	LoyerRefMajMeubleEURPerM2HC float64 `json:"loyer_ref_maj_meuble_eur_per_m2_hc"`

	// LoyerRefMeubleEURPerM2HC is the loyer de référence of a furnished letting
	// on the same cells, EUR/m²/month HC (the meublé reference, ~20 % below
	// LoyerRefMajMeubleEURPerM2HC). Zero under the same conditions.
	LoyerRefMeubleEURPerM2HC float64 `json:"loyer_ref_meuble_eur_per_m2_hc"`

	// Zone is the human-readable zone label ("Paris 11e", "Lyon 3e",
	// "Villeurbanne"). Empty when no match.
	Zone string `json:"zone,omitempty"`

	// ZoneSource is one of "paris" | "plaine_commune" | "est_ensemble" |
	// "lyon_villeurbanne". Empty when no match.
	ZoneSource string `json:"zone_source,omitempty"`

	// Confidence is "medium" on a clean match (zone and rooms bucket both
	// resolved; the grille gives a single value with no per-cell sample
	// size) and "low" on a reading the inputs left ambiguous: a multi-zone
	// EPT commune queried without coordinates, or a listing with no Rooms,
	// whose cap was collapsed across the spanned cells. ConfidenceNone
	// otherwise. Evidence.Piece / Epoque say which axes were pinned.
	Confidence string `json:"confidence"`

	// Regulation says what the law does at this address, which a zero cap
	// alone cannot: one of the Regulation* constants. RegulationCapped when
	// the Result carries the cap; RegulationNotCovered when the address lies
	// in a decreed rent-cap territory whose grille this Source does not ship
	// (its rent IS capped, by a figure unknown here, so never read the zero
	// caps as "no cap"); RegulationPartialCommune when the decree covers only
	// part of the commune and the Source cannot tell which. Empty when the
	// address lies in no decreed territory, or carries neither an INSEE nor
	// a zip the Source can place.
	Regulation string `json:"regulation,omitempty"`

	// Territory is the decreed rent-cap territory behind a
	// RegulationNotCovered or RegulationPartialCommune reading ("Bordeaux",
	// "Pays basque", "Grenoble-Alpes Métropole"). Empty otherwise: a capped
	// reading names its zone in Zone / ZoneSource.
	Territory string `json:"territory,omitempty"`

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
	// cells ("4 et plus") match any piece ≥ their stored Piece. Zero when
	// Listing.Rooms was absent, in which case the collapse spanned every
	// bucket and the Confidence is capped at ConfidenceLow.
	Piece int `json:"piece,omitempty"`

	// BuildYear is the construction year the Source filtered the époque
	// bucket on. Zero when Listing.BuildYear was absent or implausible, in
	// which case the collapse spanned every époque.
	BuildYear int `json:"build_year,omitempty"`

	// Epoque is the published construction-period label behind the reading
	// ("Avant 1946", "1946-1970", ...). Empty when the collapse spanned
	// several époques.
	Epoque string `json:"epoque,omitempty"`

	// EpoqueUnmatched is true when a BuildYear was supplied but matched no
	// published bucket, so the Source fell back to the all-époques collapse
	// rather than reporting the address unregulated.
	EpoqueUnmatched bool `json:"epoque_unmatched,omitempty"`

	// Maison is true when the reading comes from the zone's maison grille:
	// a house listing in a zone that publishes one (Plaine Commune, Est
	// Ensemble). False for apartments, and for a house in a zone with a
	// single grille for every dwelling type (Paris, Lyon / Villeurbanne).
	Maison bool `json:"maison,omitempty"`

	// NbCellsMatched is the number of grille cells that matched the
	// (zone, pièces, époque, non-meublé, maison-or-apartment) filter.
	// Drives the median collapse of the unfurnished pair.
	NbCellsMatched int `json:"nb_cells_matched,omitempty"`

	// NbMeubleCellsMatched is the number of meublé cells behind the furnished
	// pair (LoyerRefMajMeubleEURPerM2HC, LoyerRefMeubleEURPerM2HC): the same
	// filter as NbCellsMatched with meublé in place of non-meublé. Every
	// shipped grille publishes one meublé cell per unfurnished one, so the
	// two counts agree; zero when the furnished pair is zero.
	NbMeubleCellsMatched int `json:"nb_meuble_cells_matched,omitempty"`

	// Note explains, in plain words, a reading that carries no cap although
	// the law may cap the rent (Regulation not empty and not
	// RegulationCapped): which territory and decree, and what is missing.
	// Empty otherwise.
	Note string `json:"note,omitempty"`

	// ZoneID is the resolved zone identifier inside the source: the Plaine
	// Commune / Est Ensemble zone number (e.g. "311"), the Paris quartier's
	// code_grand_quartier (e.g. "7511350") or the Lyon IRIS (e.g.
	// "693830101") when Listing.IRIS pinned one. For the ambiguous 93
	// commune-fallback path it is the "+"-joined set of the commune's zones
	// (e.g. "311+312"). Empty when the reading is an arrondissement collapse.
	ZoneID string `json:"zone_id,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the source
// has nothing to say about the listing: no cap and no Regulation reading,
// i.e. an address outside every decreed rent-cap territory (the framework
// records Status == StatusOKEmpty then). A RegulationNotCovered or
// RegulationPartialCommune reading is NOT empty even though its caps are
// zero: "this rent is capped, by an unknown figure" is data, and reporting it
// as empty would read as "no cap".
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.LoyerRefMajEURPerM2HC <= 0 && r.Regulation == ""
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
// outside an encadrement zone (no published majoré). It is the UNFURNISHED
// ceiling; a furnished rent is capped by LoyerRefMajMeubleEURPerM2HC.
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
