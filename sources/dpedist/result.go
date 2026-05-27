package dpedist

// Result is the typed payload returned by Source.Query. Exposes the
// commune-level DPE-label distribution.
type Result struct {
	// Counts maps each DPE class to the raw count of DPEs the upstream
	// indexes in the commune. Labels with zero counts are omitted.
	Counts map[Label]int `json:"counts,omitempty"`

	// Shares maps each label to its share of the total (in percent,
	// 0..100). Labels with zero counts are omitted. The sum across
	// labels equals 100 ± rounding.
	Shares map[Label]float64 `json:"shares,omitempty"`

	// NbTotal is the sum of counts across every label. The framework
	// uses this as the IsEmpty() signal.
	NbTotal int `json:"nb_total"`

	// PassoireSharePct is the combined share of classes F + G
	// (rounded to one decimal). Headline number for the "passoire
	// thermique" angle.
	PassoireSharePct float64 `json:"passoire_share_pct"`

	// EfficientSharePct is the combined share of classes A + B
	// (rounded to one decimal). The "modern stock" headline.
	EfficientSharePct float64 `json:"efficient_share_pct"`

	// Confidence is ConfidenceHigh / ConfidenceLow / ConfidenceNone
	// depending on NbTotal vs the thin-sample threshold.
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

	// URL is the upstream URL the Source actually hit. Useful for
	// audit + dashboard "open in browser" affordances.
	URL string `json:"url,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// commune carries zero DPE.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.NbTotal == 0
}

// Get returns the count for a specific label, or zero when absent.
// Convenience for callers iterating the AllLabels list.
func (r *Result) Get(l Label) int {
	if r == nil || r.Counts == nil {
		return 0
	}
	return r.Counts[l]
}

// Share returns the percentage share of a specific label, or zero
// when absent. Convenience for callers iterating AllLabels.
func (r *Result) Share(l Label) float64 {
	if r == nil || r.Shares == nil {
		return 0
	}
	return r.Shares[l]
}
