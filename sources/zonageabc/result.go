package zonageabc

// Result is the typed payload returned by Source.Query.
type Result struct {
	// Zone is the official A bis / A / B1 / B2 / C classification for the
	// commune. ZoneUnknown when the commune is missing from the dataset.
	Zone Zone `json:"zone,omitempty"`

	// TensionScore is a coarse 0..4 ordinal derived from Zone. Higher =
	// tighter market. -1 when Zone is unknown.
	TensionScore int `json:"tension_score"`

	// Confidence is ConfidenceHigh on a match (the dataset is the legal
	// reference itself), ConfidenceNone for misses.
	Confidence string `json:"confidence"`

	// Evidence captures reproducibility metadata about the query that
	// produced this Result. Not part of the wire data (json:"-") —
	// populated by Source.Query.
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query that
// produced a Result.
//
// Sidecar — not part of the wire data. Travels in-process from
// Source.Query to the adapter.
type Evidence struct {
	// INSEE is the 5-digit commune code the Source consumed from
	// Listing.INSEE. May be an arrondissement code (75111, 69383,
	// 13208).
	INSEE string `json:"insee"`

	// LookupINSEE is the parent commune INSEE the Source actually
	// looked up. Differs from INSEE only on Paris / Lyon / Marseille
	// arrondissement codes that were folded to 75056 / 69123 / 13055.
	// Empty when no folding occurred (INSEE was already the parent).
	LookupINSEE string `json:"lookup_insee,omitempty"`

	// EffectiveDate is the ISO date (YYYY-MM-DD) of the underlying
	// arrêté the dataset reflects. Drawn from the embedded manifest.
	EffectiveDate string `json:"effective_date,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// commune was not found in the dataset — the framework records
// Status == StatusOKEmpty in this case.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.Zone == ZoneUnknown
}
