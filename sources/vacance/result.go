// Package vacance ports the rental enricher's commune vacancy-rate
// lookup into a standalone gazetteer Source. Given a Listing the
// Source resolves the commune INSEE and returns the LOVAC-derived
// taux de logements vacants + taux de vacance longue (≥ 2 ans).
//
// The Source is fully offline: the vacance CSV ships embedded under
// `data/`.
package vacance

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers can match on them without importing this
// package's constants.
const (
	ConfidenceHigh = "high"
	ConfidenceNone = ""
)

// Result is the typed payload returned by Source.Query. Exposes the
// commune-level vacancy + long-term vacancy split.
type Result struct {
	// VacancePct is the taux de logements vacants 2025 in the parc
	// privé (%). Zero when the commune was filtered out at LOVAC
	// ingestion (small commune with masked statistics — "secret
	// statistique").
	VacancePct float64 `json:"vacance_pct"`

	// VacanceLongPct is the taux de logements vacants > 2 ans 2025
	// (%). Zero when the upstream did not publish a long-term split.
	VacanceLongPct float64 `json:"vacance_long_pct,omitempty"`

	// Confidence is "high" when a row was found (LOVAC is a direct
	// observation, not an estimate), ConfidenceNone otherwise.
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
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// commune was filtered out at LOVAC ingestion — the framework
// records Status == StatusOKEmpty in this case.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.VacancePct <= 0
}
