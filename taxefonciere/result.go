// Package taxefonciere ports the rental enricher's taxe foncière
// estimators (V2 DGFiP taux votés + TEOM breakdown, V1 per-m² ratio
// legacy fallback) into a standalone gazetteer Source.
//
// Given a Listing the Source resolves the commune INSEE, looks up the
// DGFiP voted TFPB/TEOM taux for the commune (or dept median
// fallback), and computes the yearly TF the landlord pays plus the
// separately-itemised TEOM (recoverable from the tenant). When the
// V2 index has no signal at all (commune + dept both missing) the
// Source falls back to the legacy V1 per-m² ratio.
//
// The Source is fully offline: both embedded datasets ship under
// `data/`.
package taxefonciere

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers can match on them without importing this
// package's constants.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
	ConfidenceNone   = ""
)

// Result is the typed payload returned by Source.Query. Mirrors the
// PropertyTaxV2Estimate shape currently persisted by encheridor's
// rental enricher (with the V1 fallback fields surfaced so the
// wrapper can mirror the existing wire format byte-for-byte).
//
// Envelope-only fields are NOT part of the gazetteer payload — those
// are the framework's responsibility.
type Result struct {
	// EstimatedEURPerYear is the TF the landlord pays out-of-pocket
	// (TFPB leg only — TEOM is recoverable). Zero when no signal
	// could be derived.
	EstimatedEURPerYear float64 `json:"estimated_eur_per_year"`

	// TEOMEURPerYear is the recoverable TEOM (€/an). Surfaced
	// separately so the UI can render it as "récupérable locataire"
	// without polluting net cashflow. Zero on REOM communes.
	TEOMEURPerYear float64 `json:"teom_eur_per_year,omitempty"`

	// TauxTFPBApplied is the voted TFPB rate in percent (e.g. 32.5 =
	// 32.5 %). Echoed for traceability.
	TauxTFPBApplied float64 `json:"taux_tfpb_applied,omitempty"`

	// TauxTEOMApplied is the voted TEOM rate in percent. Zero when
	// the commune is on REOM (lump-sum) rather than TEOM.
	TauxTEOMApplied float64 `json:"taux_teom_applied,omitempty"`

	// VLEURPerM2 is the per-m² valeur locative tariff actually
	// applied. For V2 estimates this is the national VLC tariff
	// (~90 €/m²/an); for V1 fallback estimates this is the per-m² TF
	// ratio drawn from the legacy DGFiP "Tarifs des locaux
	// d'habitation" CSV.
	VLEURPerM2 float64 `json:"vl_eur_per_m2"`

	// UsedDeptFallback is true when the per-commune row was missing
	// and the Source fell back to the dept median taux.
	UsedDeptFallback bool `json:"used_dept_fallback,omitempty"`

	// UsedV1Fallback is true when the V2 fiscalité-locale index had
	// no signal at all (commune + dept both missing) and the Source
	// fell back to the V1 per-m² ratio. The TEOM fields are zero in
	// this branch since V1 does not separate TFPB from TEOM.
	UsedV1Fallback bool `json:"used_v1_fallback,omitempty"`

	// Confidence is one of "high" / "medium" / "low" — High when
	// commune-row V2 hit, Medium on dept fallback OR V1 commune hit,
	// Low on V1 dept fallback, ConfidenceNone when no data.
	Confidence string `json:"confidence"`

	// Evidence captures reproducibility metadata about the query that
	// produced this Result.
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query that
// produced a Result.
//
// Sidecar — not part of the wire data. Travels in-process from
// Source.Query to the adapter.
type Evidence struct {
	// INSEE is the 5-digit commune code the Source filtered on.
	INSEE string `json:"insee"`

	// SurfaceM2 is the habitable surface (m²) consumed in the
	// estimate. Zero when missing.
	SurfaceM2 float64 `json:"surface_m2,omitempty"`

	// PathUsed records which branch ran: "v2_commune", "v2_dept",
	// "v1_commune", "v1_dept", or "" when no signal.
	PathUsed string `json:"path_used,omitempty"`

	// VLCAbattement is the abattement applied to the VLC tariff
	// (typically 0.5 per CGI art. 1388).
	VLCAbattement float64 `json:"vlc_abattement,omitempty"`

	// V2DataYear is the year stamp of the fiscalité-locale dataset.
	V2DataYear int `json:"v2_data_year,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when no TF
// signal could be derived at all (V2 + V1 both missed).
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.EstimatedEURPerYear <= 0 && r.TEOMEURPerYear <= 0
}
