// Package qpv ships an offline Source that flags whether a commune
// hosts at least one Quartier Prioritaire de la Politique de la Ville
// (QPV) and lists those QPVs.
//
// QPV is the official zoning policy administered by the ANCT
// (decree 2023-1314, effective 1 January 2024) that designates the
// most disadvantaged urban neighbourhoods. The label gates several
// fiscal and social devices: rental investors targeting QPV-located
// stock face specific guardrails (Pinel restrictions, exonérations
// TFPB, ZFU exemptions) and a different tenant demographic.
//
// IMPORTANT: this Source operates at the commune granularity — it
// answers "does this commune contain QPVs?" but NOT "is this address
// in a QPV?". For address-level QPV membership, callers must hit
// the ANCT SIG Ville API (sig.ville.gouv.fr) which requires
// authentication.
//
// The Source is fully offline: the QPV → commune mapping ships
// embedded under `data/`.
package qpv

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers can match on them without importing this
// package's constants.
const (
	ConfidenceHigh = "high"
	ConfidenceNone = ""
)

// QPV is one Quartier Prioritaire — a code (e.g. "QN07501M") and a
// human-readable label.
type QPV struct {
	// Code is the official ANCT identifier — format "QNXXXYYZ" where
	// XX is the department code, YY the order within the department,
	// Z a single-letter suffix.
	Code string `json:"code"`

	// Label is the QPV's official name (e.g. "Belleville",
	// "La Duchère").
	Label string `json:"label,omitempty"`
}

// Result is the typed payload returned by Source.Query.
type Result struct {
	// HasQPV is true when the commune hosts at least one QPV.
	HasQPV bool `json:"has_qpv"`

	// QPVCount is the number of QPVs in the commune.
	QPVCount int `json:"qpv_count,omitempty"`

	// QPVs is the list of QPVs in the commune (code + label).
	QPVs []QPV `json:"qpvs,omitempty"`

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
	// INSEE is the 5-digit commune code the Source filtered on.
	INSEE string `json:"insee"`

	// CommuneLabel is the human-readable commune name from the source
	// file. Useful for logging / diagnostics; not always populated.
	CommuneLabel string `json:"commune_label,omitempty"`

	// RowCountCommunes is the total number of communes hosting at
	// least one QPV. Sanity scalar for downstream renderers.
	RowCountCommunes int `json:"row_count_communes,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// commune hosts no QPV — the framework records Status == StatusOKEmpty
// in this case.
//
// The vast majority of French communes are QPV-free, so most queries
// will report IsEmpty().
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return !r.HasQPV
}
