package zonetendue

// Result is the typed payload returned by Source.Query.
type Result struct {
	// Tier is the commune's classification. TierNonTendue when the
	// commune is outside the zonage (absence from the embedded
	// dataset).
	Tier Tier `json:"tier"`

	// IsTendue is a convenience boolean: true when Tier is one of
	// TierTendue or TierTendueTouristique. Drives the legal
	// consequences listed in the package doc (notice period, TLV,
	// THRS).
	IsTendue bool `json:"is_tendue"`

	// FlaggedTLV2013 reports whether the commune was on the original
	// TLV décret 2013 list. The 2023 / 2025 revisions reshaped the
	// classification ; a commune may be tendue today without having
	// been on the original TLV list, and vice versa.
	FlaggedTLV2013 bool `json:"flagged_tlv_2013,omitempty"`

	// Confidence is always ConfidenceHigh (the source dataset is the
	// legal reference). It stays ConfidenceHigh even when no row was
	// found, because "no row" maps to TierNonTendue, which is itself
	// the legally correct answer for those communes.
	Confidence string `json:"confidence"`

	// Evidence captures reproducibility metadata about the query that
	// produced this Result. Not part of the wire data (json:"-") —
	// populated by Source.Query.
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query.
type Evidence struct {
	// INSEE is the 5-digit commune code the Source consumed from
	// Listing.INSEE.
	INSEE string `json:"insee"`

	// EffectiveDate is the ISO date (YYYY-MM-DD) of the underlying
	// décret the dataset reflects.
	EffectiveDate string `json:"effective_date,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter.
//
// Note on semantics: for this Source, "empty" means "the commune was
// not found in the index", which the framework folds into a
// StatusOKEmpty signal. BUT consumers should not interpret IsEmpty as
// "no answer" — the absence itself IS the answer (non_tendue). Tier
// is always populated and TierNonTendue is a valid, complete reading.
//
// IsEmpty returns true ONLY when the Result is structurally empty (no
// Tier set — should not happen on the happy path; defensive only).
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.Tier == ""
}
