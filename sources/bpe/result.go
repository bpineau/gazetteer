package bpe

// Result is the typed payload returned by Source.Query. Exposes the
// curated per-commune equipment counts.
type Result struct {
	// Counts maps each Bucket to the integer count of equipments of
	// that bucket present in the commune. Zero counts are omitted.
	Counts map[Bucket]int `json:"counts,omitempty"`

	// TotalFacilities is the sum across every Bucket. Surfaced so the
	// UI can render "N équipements" without summing client-side.
	TotalFacilities int `json:"total_facilities"`

	// Confidence is "high" when the commune was found with at least
	// one equipment in the curated subset, ConfidenceNone otherwise.
	Confidence string `json:"confidence"`

	// Evidence captures reproducibility metadata about the query that
	// produced this Result. Not part of the wire data (json:"-") —
	// populated by Source.Query.
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query.
type Evidence struct {
	// INSEE is the 5-digit commune code the Source filtered on.
	INSEE string `json:"insee"`

	// ReferenceDate is the source's reference date from the embedded
	// manifest (e.g. "2024-01-01").
	ReferenceDate string `json:"reference_date,omitempty"`

	// RowCountCommunes is the total number of communes with at least
	// one indexed facility. Sanity scalar for downstream renderers.
	RowCountCommunes int `json:"row_count_communes,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// commune was not in the embedded index (no curated facility found).
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.TotalFacilities == 0
}

// Get returns the count for a specific bucket, or zero when absent.
// Convenience for callers iterating the AllBuckets list.
func (r *Result) Get(b Bucket) int {
	if r == nil || r.Counts == nil {
		return 0
	}
	return r.Counts[b]
}
