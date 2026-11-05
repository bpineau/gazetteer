// Package taxefonciere provides a gazetteer.Source that estimates the
// yearly taxe foncière a landlord pays on a French dwelling.
//
// Given a Listing the Source resolves the commune INSEE, looks up the
// DGFiP voted TFPB/TEOM taux for the commune (or dept median
// fallback), and computes the yearly TF the landlord pays plus the
// separately-itemised TEOM (recoverable from the tenant). The V2 path
// (DGFiP taux votés + TEOM breakdown) is the only one that estimates; the
// V1 per-m² valeur locative is read only when V2 has no signal at all
// (commune + dept both missing), and then reported without an estimate.
//
// LIMITATION — this is an order-of-magnitude estimate, not the exact bill.
// The voted taux are real, but they are applied to a valeur-locative *proxy*
// (a per-m² tariff), not the dwelling's actual cadastral base. The proxy
// systematically understates the tax in high-value communes — notably Paris,
// where the figure can be roughly half the real amount. Use EstimatedEURPerYear
// for relative comparison between communes, not as the precise sum due. (A
// per-commune REI base would fix this; it is deferred.)
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

// Result is the typed payload returned by Source.Query. Exposes both
// the V2 (taux votés) fields and the V1 (per-m² ratio) fallback fields
// so downstream consumers can render either path cleanly.
//
// Envelope-only fields are NOT part of this payload — those are the
// framework's responsibility (see gazetteer.Result).
type Result struct {
	// EstimatedEURPerYear is the TF the landlord pays out-of-pocket
	// (TFPB leg only — TEOM is recoverable), €/an. Zero when no signal
	// could be derived. APPROXIMATION: taux votés × a valeur-locative
	// *proxy* × surface — it can materially understate the real bill in
	// high-value communes (notably Paris, ~½ the actual). Treat it as a
	// comparison signal, not the exact amount (see package doc).
	EstimatedEURPerYear float64 `json:"estimated_eur_per_year"`

	// TEOMEURPerYear is the recoverable TEOM (€/an). Surfaced
	// separately so the UI can render it as "récupérable locataire"
	// without polluting net cashflow. Zero on REOM communes.
	TEOMEURPerYear float64 `json:"teom_eur_per_year,omitempty"`

	// TauxTFPBApplied is the voted TFPB rate in percent (e.g. 32.5 =
	// 32.5 %). Echoed for traceability. Zero means no voted rate was
	// available (a V1 fallback, which carries no estimate; not a 0 % tax rate) — consumers
	// must treat 0 as "no reading" for this field specifically.
	TauxTFPBApplied float64 `json:"taux_tfpb_applied,omitempty"`

	// TauxTEOMApplied is the voted TEOM rate in percent. Zero when
	// the commune is on REOM (lump-sum) rather than TEOM.
	TauxTEOMApplied float64 `json:"taux_teom_applied,omitempty"`

	// VLEURPerM2 is the per-m² valeur locative tariff actually
	// applied. For V2 estimates this is the national VLC tariff
	// (~90 EUR/m²/year); on the V1 fallback it is the commune's median
	// valeur locative cadastrale per m² from the DGFiP "Tarifs des locaux
	// d'habitation" export, a tax base before rate and abattement.
	VLEURPerM2 float64 `json:"vl_eur_per_m2"`

	// UsedDeptFallback is true when the per-commune row was missing
	// and the Source fell back to the dept median taux.
	UsedDeptFallback bool `json:"used_dept_fallback,omitempty"`

	// UsedV1Fallback is true when the V2 fiscalité-locale index had
	// no signal at all (commune + dept both missing) and the Source read
	// the V1 per-m² valeur locative instead. That tariff has no rate to
	// apply, so the branch carries no estimate: EstimatedEURPerYear and
	// TEOMEURPerYear are 0 and Confidence is ConfidenceNone.
	UsedV1Fallback bool `json:"used_v1_fallback,omitempty"`

	// Confidence is "high" on a commune-row V2 hit, "medium" on the V2
	// département fallback, ConfidenceNone when there is no estimate (no
	// data, or the V1 fallback).
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
