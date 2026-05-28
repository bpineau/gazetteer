package rpls

// Result is the typed payload returned by Source.Query.
type Result struct {
	// LLSRate is the share of logements locatifs sociaux over résidences
	// principales (loi SRU computation), expressed as a percentage in
	// [0, 100+]. Communes can exceed 100 in rare cases when the SRU
	// inventory transiently overshoots the RP count; rendered verbatim.
	LLSRate float64 `json:"lls_rate"`

	// Tier is the distribution-relative bucket (rural / mixte / fort /
	// satured). TierUnknown when the commune is missing from the dataset.
	Tier Tier `json:"tier,omitempty"`

	// Confidence is "high" when the commune was located in the dataset,
	// ConfidenceNone otherwise. The upstream itself stamps 0 % for many
	// communes never subject to the SRU obligation — those still return
	// ConfidenceHigh + LLSRate=0 + TierRural; only communes absent from
	// the embedded crosswalk surface as Confidence=ConfidenceNone.
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
	// INSEE is the 5-digit commune code the Source filtered on (after
	// Paris / Lyon / Marseille folding).
	INSEE string `json:"insee"`

	// CommuneLabel is the human-readable commune name from the dataset.
	// Useful for logging / diagnostics; not always populated.
	CommuneLabel string `json:"commune_label,omitempty"`

	// DataYear is the vintage of the upstream dataset.
	DataYear int `json:"data_year,omitempty"`

	// RowCountCommunes is the total number of communes in the embedded
	// crosswalk. Sanity scalar for downstream renderers.
	RowCountCommunes int `json:"row_count_communes,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// commune was missing from the embedded crosswalk — the framework
// records Status == StatusOKEmpty in this case.
//
// A populated entry with LLSRate=0 (rural commune below SRU obligation)
// is NOT empty — the answer "this commune carries no SRU social housing"
// is a real reading.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.Confidence == ConfidenceNone
}
