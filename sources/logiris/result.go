// Package logiris is a gazetteer.Source that returns INSEE census
// housing-structure indicators at the IRIS (sub-commune) level, keyed by
// the listing's resolved IRIS code: the share of renters, the share of
// social housing, and the vacancy rate of the neighbourhood.
//
// Where the commune-level `vacance` source answers "how vacant is this
// town", logiris paints the *neighbourhood* rental market — a high renter
// share + low vacancy marks a deep, liquid rental zone (easy to let), and
// the social-housing share flags how much of the stock sits outside the
// private market. These vary sharply across the IRIS of a single dense
// commune, exactly the IDF zones an investor weighs.
//
// The Source is fully offline (gzipped JSON embedded under `data/`) and
// IDF-scoped (Île-de-France IRIS, matching the `iris` resolver). It only
// answers for listings whose Listing.IRIS is populated.
package logiris

// Confidence values returned in Result.Confidence.
const (
	ConfidenceHigh = "high"
	ConfidenceNone = ""
)

// Result is the typed payload returned by Source.Query.
type Result struct {
	// RenterSharePct is the share of résidences principales occupied by
	// tenants (P21_RP_LOC / P21_RP, %). High = a rental-oriented zone.
	RenterSharePct float64 `json:"renter_share_pct"`

	// SocialHousingSharePct is the share of résidences principales that are
	// rented social housing (P21_RP_LOCHLMV / P21_RP, %).
	SocialHousingSharePct float64 `json:"social_housing_share_pct,omitempty"`

	// VacancyRatePct is the IRIS vacancy rate (P21_LOGVAC / P21_LOG, %).
	// Low = a tight market.
	VacancyRatePct float64 `json:"vacancy_rate_pct"`

	// TotalLogements is the IRIS dwelling count (P21_LOG, rounded) — a
	// sample-size sanity scalar.
	TotalLogements int `json:"total_logements,omitempty"`

	// Confidence is "high" on a populated IRIS, ConfidenceNone otherwise.
	Confidence string `json:"confidence"`

	// Evidence captures reproducibility metadata. Sidecar — not wire data.
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query.
type Evidence struct {
	// IRIS is the 9-digit IRIS code the Source filtered on.
	IRIS string `json:"iris"`

	// DataYear is the census reference year (e.g. 2021).
	DataYear int `json:"data_year,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter: true when the IRIS was not
// found / has no dwellings (TotalLogements <= 0).
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.TotalLogements <= 0
}
