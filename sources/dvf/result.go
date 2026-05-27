package dvf

import (
	"fmt"

	"github.com/bpineau/gazetteer/appraisal"
)

// Result is the typed payload returned by Source.Query. Exposes the
// DVF-derived per-m² and total value (in cents) plus p25/p75
// quartiles, sample size and a confidence string.
//
// Envelope-only fields (schema_version, source_version, computed_at,
// input_hash) are NOT part of this payload — those are the framework's
// responsibility (see gazetteer.Result).
type Result struct {
	// ValueEURPerM2Cents is the median price-per-m² over the filtered
	// mutations, in centimes (NOT euros). Nil when the sample is
	// empty / median is 0.
	ValueEURPerM2Cents *int64 `json:"value_eur_per_m2_centimes"`

	// ValueEURCents is the total estimated property value in centimes,
	// computed as ValueEURPerM2Cents × surface_m2. Nil when the surface
	// or per-m² value is unknown.
	ValueEURCents *int64 `json:"value_eur_centimes,omitempty"`

	// P25EURPerM2Cents / P75EURPerM2Cents are the 25th and 75th
	// percentiles of the filtered price-per-m² distribution, in
	// centimes. Nil when the sample is empty.
	P25EURPerM2Cents *int64 `json:"p25_eur_per_m2_centimes,omitempty"`
	P75EURPerM2Cents *int64 `json:"p75_eur_per_m2_centimes,omitempty"`

	// SampleSize is the number of mutations behind ValueEURPerM2Cents.
	SampleSize int `json:"sample_size"`

	// Confidence is one of "high" / "medium" / "low" per PickConfidence.
	Confidence string `json:"confidence"`

	// Evidence captures reproducibility metadata about the query that
	// produced this Result. Not part of the wire data (json:"-") —
	// populated by Source.Query, consumed in-process by callers that
	// need to log or audit how the answer was derived (e.g.
	// a downstream payload's method params).
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query that
// produced a Result. Consumers that need to log or audit how the answer
// was derived (e.g. a downstream payload's method params) read
// these fields. Other callers can ignore them.
//
// Sidecar — not part of the wire data. Travels in-process from
// Source.Query to the adapter.
type Evidence struct {
	// LevelUsed is the winning ladder rung that produced the result —
	// "address_radius", "commune", "neighborhood", or "department".
	LevelUsed string `json:"level_used"`

	// CommunesQueried is the list of INSEE codes the winning tier
	// fanned out over. Always [primary] for address_radius and
	// commune; multiple for neighborhood / department.
	CommunesQueried []string `json:"communes_queried"`

	// PrimaryINSEE is the auction's resolved INSEE code (the listing's
	// commune, regardless of which tier won).
	PrimaryINSEE string `json:"primary_insee"`

	// INSEEResolutionSource records which step of the INSEE cascade
	// produced PrimaryINSEE: "ban_forward" or "ban_reverse" (cf.
	// helpers/banx/insee_resolver.go). Empty when the
	// listing carried a usable INSEE directly.
	INSEEResolutionSource string `json:"insee_resolution_source,omitempty"`

	// TypeLocalFilter is the DVF `type_local` value the filter applied
	// (e.g. "Appartement", "Maison").
	TypeLocalFilter string `json:"type_local_filter"`

	// WindowYears is the lookback window in years applied to
	// `date_mutation` (today: CutoffYears).
	WindowYears int `json:"window_years"`

	// RawMutationsCount is the total mutation count returned by the
	// upstream for the winning tier's fan-out, BEFORE
	// FilterMutations is applied.
	RawMutationsCount int `json:"raw_mutations_count"`

	// FilteredMutationsCount is the post-FilterMutations sample size
	// the winning tier produced (= Result.SampleSize).
	FilteredMutationsCount int `json:"filtered_mutations_count"`

	// SectionsQueried is the cumulative number of cadastral sections
	// the winning tier fanned out over.
	SectionsQueried int `json:"sections_queried"`

	// RadiusM is the disk radius (in metres) the `address_radius`
	// tier applied around (AuctionLat, AuctionLon) to filter the
	// commune's mutations. 0 when the winning tier is not
	// `address_radius`.
	RadiusM float64 `json:"radius_m,omitempty"`

	// AuctionLat / AuctionLon are the property's geocoded
	// coordinates passed through from the listing. Populated
	// only when the winning tier is `address_radius` (where they
	// are load-bearing); omitted otherwise to keep the params blob
	// stable.
	AuctionLat *float64 `json:"auction_lat,omitempty"`
	AuctionLon *float64 `json:"auction_lon,omitempty"`

	// NUniqueParcelles is the number of distinct `id_parcelle`
	// values backing FilteredMutationsCount, after the
	// MaxMutationsPerParcelle cap. Surfaced so downstream callers
	// (UI, doctor predicates) can flag a low-diversity winning tier.
	NUniqueParcelles int `json:"n_unique_parcelles,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when DVF
// found no comparable mutations for the listing — the framework
// records Status == StatusOKEmpty in this case.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.SampleSize == 0
}

// PriceEstimate satisfies appraisal.PriceEstimator. Returns a zero-value
// estimate when ValueEURPerM2Cents is nil (empty sample) — callers
// should also check IsEmpty before relying on the estimate.
//
// Method follows the "dvf_<level>_<window>y" convention so downstream
// auditors can tell at a glance which tier won and over how many years.
func (r *Result) PriceEstimate() appraisal.PriceEstimate {
	var v int64
	if r.ValueEURPerM2Cents != nil {
		v = *r.ValueEURPerM2Cents
	}
	return appraisal.PriceEstimate{
		EurPerM2Cents: v,
		Confidence:    mapDVFConfidence(r.Confidence),
		SampleSize:    r.SampleSize,
		Method: fmt.Sprintf("dvf_%s_%dy",
			nonEmptyOr(r.Evidence.LevelUsed, "unknown"),
			r.Evidence.WindowYears),
	}
}

// mapDVFConfidence translates DVF's stable confidence strings to the
// appraisal package's coarse enum. Unknown values map to Low so callers
// downstream never panic on a future DVF label.
func mapDVFConfidence(s string) appraisal.Confidence {
	switch s {
	case ConfidenceHigh:
		return appraisal.ConfidenceHigh
	case ConfidenceMedium:
		return appraisal.ConfidenceMedium
	default:
		return appraisal.ConfidenceLow
	}
}

func nonEmptyOr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
