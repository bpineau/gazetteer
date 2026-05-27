package chomage

// Result is the typed payload returned by Source.Query. Exposes the
// commune's zone d'emploi, its latest seasonally-adjusted unemployment
// rate and a short trend window.
type Result struct {
	// ZECode is the 4-digit INSEE zone d'emploi 2020 code the commune
	// belongs to. Empty when the commune is missing from the crosswalk.
	ZECode string `json:"ze_code,omitempty"`

	// ZELabel is the human-readable name of the zone d'emploi (e.g.
	// "Paris", "Bourg-en-Bresse"). Empty when the commune is missing.
	ZELabel string `json:"ze_label,omitempty"`

	// QuarterLabel is the source's identifier for the reference quarter
	// of the headline RatePct value, formatted "YYYY-Tn" (e.g. "2025-T4").
	// Empty when the commune is missing from the crosswalk.
	QuarterLabel string `json:"quarter_label,omitempty"`

	// RatePct is the latest seasonally-adjusted unemployment rate (%)
	// observed in the zone d'emploi. Zero when the commune is missing
	// from the embedded crosswalk; a real rate is always > 0 in metro
	// France.
	RatePct float64 `json:"rate_pct"`

	// NationalRatePct is the national average across the 302 ZEs at
	// QuarterLabel. Stamped on every populated Result so callers can
	// compute the delta without parsing the manifest themselves.
	NationalRatePct float64 `json:"national_rate_pct,omitempty"`

	// DeltaVsNationalPP is RatePct − NationalRatePct expressed in
	// percentage points (positive = local zone worse than national).
	DeltaVsNationalPP float64 `json:"delta_vs_national_pp,omitempty"`

	// Tension is the peer-relative tension bucket. TensionUnknown when
	// the commune is missing from the crosswalk.
	Tension TensionFlag `json:"tension,omitempty"`

	// RecentTrendSeries carries the last quarter values of the rate
	// (oldest first), aligned with the matching slice in Evidence.
	// Useful for UI sparklines without exposing the full 20-quarter
	// embed.
	RecentTrendSeries []float64 `json:"recent_trend_series,omitempty"`

	// Confidence is "high" when the commune was located in the
	// crosswalk and a numeric rate was found, ConfidenceNone otherwise.
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
	// INSEE is the 5-digit commune code the Source filtered on. Drawn
	// from Listing.INSEE (mandatory).
	INSEE string `json:"insee"`

	// SeriesStart / SeriesEnd are the bounds of the embedded quarterly
	// time series — useful for callers that want to render the trend
	// axis on a sparkline.
	SeriesStart string `json:"series_start,omitempty"`
	SeriesEnd   string `json:"series_end,omitempty"`

	// QuarterLabels aligns 1:1 with RecentTrendSeries. Surfaced on the
	// sidecar to keep the wire Result compact while still letting
	// auditors line each value up with its quarter.
	QuarterLabels []string `json:"quarter_labels,omitempty"`

	// RowCountZones / RowCountCommunes are sanity scalars for renderers.
	RowCountZones    int `json:"row_count_zones,omitempty"`
	RowCountCommunes int `json:"row_count_communes,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// commune was missing from the crosswalk OR the located zone had no
// numeric reading for the latest quarter (DOM-COM edge cases).
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.RatePct <= 0
}
