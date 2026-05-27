package anct

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers can match on them without importing this
// package's constants.
const (
	ConfidenceHigh = "high"
	ConfidenceNone = ""
)

// Result is the typed payload returned by Source.Query.
type Result struct {
	// ACV is true when the commune belongs to the Action Cœur de Ville
	// programme.
	ACV bool `json:"acv"`

	// PVD is true when the commune belongs to the Petites Villes de
	// Demain programme.
	PVD bool `json:"pvd"`

	// ORT is true when the commune has signed an Opération de
	// Revitalisation de Territoire convention. ORT signature unlocks
	// the Denormandie tax device on renovated rental investments.
	ORT bool `json:"ort"`

	// Programmes is the list of programme handles the commune
	// participates in. Useful for renderers that want a compact list
	// instead of three booleans. Ordered as: ACV, PVD, ORT.
	Programmes []string `json:"programmes,omitempty"`

	// DenormandieEligible mirrors ORT — kept as a separate field so
	// callers can express the rental-investor intent directly.
	DenormandieEligible bool `json:"denormandie_eligible"`

	// ACVSignedAt / PVDSignedAt / ORTSignedAt carry the signature
	// date (YYYY-MM-DD) when the upstream provided one. Empty
	// otherwise.
	ACVSignedAt string `json:"acv_signed_at,omitempty"`
	PVDSignedAt string `json:"pvd_signed_at,omitempty"`
	ORTSignedAt string `json:"ort_signed_at,omitempty"`

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

	// RowCountCommunes is the total number of communes flagged for at
	// least one programme. Sanity scalar for downstream renderers.
	RowCountCommunes int `json:"row_count_communes,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// commune participates in none of the three programmes — the
// framework records Status == StatusOKEmpty in this case.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return !r.ACV && !r.PVD && !r.ORT
}

// programmeList renders the boolean trio as a compact ordered slice.
func programmeList(acv, pvd, ort bool) []string {
	out := make([]string, 0, 3)
	if acv {
		out = append(out, "acv")
	}
	if pvd {
		out = append(out, "pvd")
	}
	if ort {
		out = append(out, "ort")
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
