package ips_ecoles

// Result is the typed payload returned by Source.Query.
type Result struct {
	// IPSMedian is the unweighted median IPS over the commune's
	// écoles primaires.
	IPSMedian float64 `json:"ips_median"`

	// IPSMin is the minimum IPS observed across the commune's schools.
	// Useful as a heterogeneity signal — a wide IPSMin/IPSMax band
	// flags a mixed catchment even when the median looks balanced.
	IPSMin float64 `json:"ips_min,omitempty"`

	// IPSMax is the maximum IPS observed across the commune's schools.
	IPSMax float64 `json:"ips_max,omitempty"`

	// SchoolCount is the number of écoles primaires the median was
	// computed over.
	SchoolCount int `json:"school_count,omitempty"`

	// Tier is the distribution-relative bucket (precaire / mixte /
	// moyen / favorise). TierUnknown when the commune is missing from
	// the dataset.
	Tier Tier `json:"tier,omitempty"`

	// Confidence reflects the sample size:
	//   - "high"   : SchoolCount ≥ 3
	//   - "medium" : SchoolCount ∈ {1, 2}
	//   - ""       : commune absent from the dataset
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
	// INSEE is the 5-digit commune code the Source filtered on. The
	// ips_ecoles source does NOT fold arrondissements — Paris, Lyon
	// and Marseille arrondissements carry their own rows.
	INSEE string `json:"insee"`

	// DataYearLabel is the rentrée-scolaire label of the upstream
	// dataset (e.g. "2024-2025").
	DataYearLabel string `json:"data_year_label,omitempty"`

	// RowCountCommunes is the total number of communes in the embedded
	// dataset (communes hosting ≥ 1 école).
	RowCountCommunes int `json:"row_count_communes,omitempty"`

	// RowCountSchools is the total number of écoles in the embedded
	// dataset across every commune.
	RowCountSchools int `json:"row_count_schools,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// commune was missing from the embedded dataset — typically a rural
// commune that hosts no école.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.Confidence == ConfidenceNone
}
