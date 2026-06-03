package sitadel

// Confidence levels stamped on a Result.
const (
	// ConfidenceHigh marks a commune located in the embedded dataset with a
	// populated recent authorised series.
	ConfidenceHigh = "high"
	// ConfidenceNone marks a commune absent from the dataset, or present
	// with no non-zero authorised data.
	ConfidenceNone = ""
)

// Result is the typed payload returned by Source.Query: per-commune
// housing-construction dynamics derived from the SDES Sitadel annual file
// (building permits authorised + housing starts). All counts are dwellings
// ("logements"), not floor area.
//
// The signal is forward-looking SUPPLY: how much new housing a commune is
// permitting (authorised) and breaking ground on (started). For a rental
// investor a large incoming pipeline relative to the existing stock is a
// headwind on rents and resale, while near-zero construction in a tense
// market reinforces scarcity. Absolute counts scale with commune size — the
// per-stock normalisation that turns these into a comparable rate belongs in
// the appraisal layer, so this Source stays deliberately raw.
type Result struct {
	// LatestYear is the most recent year present for the commune (the year
	// of AuthorizedLatest). Provisional millésimes are included.
	LatestYear int `json:"latest_year,omitempty"`

	// AuthorizedLatest is "Tous Logements" dwellings AUTHORISED (permits,
	// LOG_AUT) in LatestYear. Count of dwellings.
	AuthorizedLatest int `json:"authorized_latest"`

	// StartedLatest is "Tous Logements" dwellings STARTED (commencés,
	// LOG_COM) in the most recent year that carries a started value. The
	// freshest millésime is provisional and publishes no starts, so this is
	// typically one year behind LatestYear — see StartedLatestYear. Count of
	// dwellings.
	StartedLatest int `json:"started_latest"`

	// StartedLatestYear is the year of StartedLatest. It can differ from
	// LatestYear because the latest millésime carries authorisations but no
	// (yet-consolidated) starts. Zero when the commune has no started value.
	StartedLatestYear int `json:"started_latest_year,omitempty"`

	// AuthorizedAvg5y is the mean per year of "Tous Logements" LOG_AUT over
	// the last 5 years that carry an authorised value (dwellings/year,
	// rounded to one decimal). More stable than a single noisy year.
	AuthorizedAvg5y float64 `json:"authorized_avg_5y"`

	// StartedAvg5y is the mean per year of "Tous Logements" LOG_COM over the
	// last 5 years that carry a started value (dwellings/year, rounded to one
	// decimal).
	StartedAvg5y float64 `json:"started_avg_5y"`

	// CollectifSharePct is the share of "Collectif" (apartment) dwellings in
	// total authorised dwellings over the recent window (percent, one
	// decimal). High share = an apartment-heavy pipeline, the segment rental
	// investors care about. Zero when no authorised dwellings in the window.
	CollectifSharePct float64 `json:"collectif_share_pct"`

	// AuthorizedSeries is the per-year "Tous Logements" LOG_AUT, oldest →
	// newest, for a sparkline. A missing year reads as 0 in the series. Pair
	// with SeriesStartYear to label the points.
	AuthorizedSeries []int `json:"authorized_series,omitempty"`

	// SeriesStartYear is the year of AuthorizedSeries[0] (oldest point).
	SeriesStartYear int `json:"series_start_year,omitempty"`

	// Confidence is ConfidenceHigh when the commune carries a populated
	// recent authorised series, ConfidenceNone otherwise.
	Confidence string `json:"confidence"`

	// Evidence captures reproducibility metadata about the query that
	// produced this Result. Not part of the wire data (json:"-") — populated
	// by Source.Query, consumed in-process by callers that audit provenance.
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query that produced a
// Result.
//
// Sidecar — not part of the wire data. Travels in-process from Source.Query
// to the adapter.
type Evidence struct {
	// INSEE is the commune code the Source looked up, AFTER folding Paris /
	// Lyon / Marseille arrondissements onto their parent commune.
	INSEE string `json:"insee"`

	// DataMillesime is the upstream millésime of the embedded file (e.g.
	// "2026-06").
	DataMillesime string `json:"data_millesime,omitempty"`

	// RowYears is the number of distinct years present for the commune in the
	// embedded dataset.
	RowYears int `json:"row_years,omitempty"`

	// RowCountCommunes is the total number of communes in the embedded
	// dataset.
	RowCountCommunes int `json:"row_count_communes,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the commune
// was missing from the embedded dataset, or present with no non-zero
// authorised data — the framework records Status == StatusOKEmpty in this
// case.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.Confidence == ConfidenceNone
}
