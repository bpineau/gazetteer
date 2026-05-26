package locservice

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers (encheridor's adapter, dashboards) can match on
// them without importing this package's constants.
const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// SkipReason sentinels populated on empty (no-match) results. Stable
// wire contract — downstream consumers group on these values.
const (
	SkipReasonNoData = "no_data"
)

// Score range observed on the LocService gauge images
// (fleche0.png..fleche8.png). Scores beyond this range are treated as
// invalid.
const (
	ScoreMin = 0
	ScoreMax = 8
)

// TensionLabel is the human-friendly bucket we map the raw arrow score
// onto.
type TensionLabel string

// TensionLabel constants are the five ordered buckets from the LocService
// "facilité à trouver une location" gauge (très détendu = most supply,
// très tendu = most demand).
const (
	LabelTresDetendu TensionLabel = "très détendu"
	LabelDetendu     TensionLabel = "détendu"
	LabelEquilibre   TensionLabel = "équilibré"
	LabelTendu       TensionLabel = "tendu"
	LabelTresTendu   TensionLabel = "très tendu"
)

// Result is the typed payload returned by Source.Query. Mirrors the JSON
// shape currently persisted by encheridor's LocService enricher
// (resultBlob with tension/budget scores + scale + description +
// confidence) so the encheridor adapter can re-serialise it 1:1 into
// its EnrichPayload.Result.
//
// Envelope-only fields (schema_version, enricher_version, computed_at,
// input_hash) are NOT part of the gazetteer payload — those are the
// framework's responsibility (Result envelope in gazetteer.Result, or
// in encheridor's enrich.EnrichPayload).
type Result struct {
	// TensionLabel is the tensiometer bucket derived from TensionScore.
	// Sentinel value LabelEquilibre is stamped on the no-data branch
	// (HasData=false) for backwards-compat with payloads written
	// before consumers learned to gate on method.params.no_data. The
	// caller MUST check NoData first.
	TensionLabel string `json:"tension_label"`

	// TensionScore is the raw 0..8 LocService arrow value for the
	// "Facilite a trouver une location" gauge (= rental supply
	// tightness; high means landlord-friendly). Nil on the no-data
	// branch.
	TensionScore *int `json:"tension_score"`

	// DemandScore is reserved (alias slot for compatibility); always
	// nil today.
	DemandScore *int `json:"demand_score,omitempty"`

	// SupplyScore mirrors TensionScore (alias: high score == landlord-
	// friendly = high supply tightness). Same nil-on-no-data rule.
	SupplyScore *int `json:"supply_score,omitempty"`

	// BudgetScore is the raw 0..8 LocService arrow value for the
	// "Budget des locataires" gauge (= tenant solvency). Nil when the
	// second arrow could not be extracted.
	BudgetScore *int `json:"budget_score,omitempty"`

	// ScoreScale documents the score range in human-readable form.
	// Wire-stable string.
	ScoreScale string `json:"score_scale"`

	// Description is the first sentence of the rendered "analyseTensio"
	// paragraph, with HTML entities resolved. May be empty.
	Description string `json:"description,omitempty"`

	// SampleSize is 1 when a measurement was extracted, 0 on the
	// no-data branch.
	SampleSize int `json:"sample_size"`

	// Confidence is one of "high" / "medium" / "low" per the
	// calibration in BuildResult.
	Confidence string `json:"confidence"`

	// Evidence captures reproducibility metadata about the query that
	// produced this Result. Not part of the wire data (json:"-") —
	// populated by Source.Query, consumed in-process by callers that
	// need to log or audit how the answer was derived (e.g.
	// encheridor's EnrichPayload.Method.Params).
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query that
// produced a Result. Consumers that need to log or audit how the answer
// was derived (e.g. encheridor's EnrichPayload.Method.Params) read
// these fields. Other callers can ignore them.
//
// Sidecar — not part of the wire data. Travels in-process from
// Source.Query to the adapter.
type Evidence struct {
	// INSEE is the 5-digit commune code the Source resolved for the
	// listing.
	INSEE string `json:"insee"`

	// Logement is the LocService keyword the Source first requested
	// (e.g. "studio", "T2", "F3"); empty for the "all types" call.
	Logement string `json:"logement"`

	// LogementUsed is the keyword that yielded data — may differ from
	// Logement when the Source fell back to "" (all-types).
	LogementUsed string `json:"logement_used"`

	// FellBack is true when the Source widened a logement-specific
	// call to "" because the first call returned no data.
	FellBack bool `json:"fell_back,omitempty"`

	// CityLabel is the commune name LocService echoed back in the
	// response header. Useful to cross-check the INSEE we sent.
	CityLabel string `json:"city_label,omitempty"`

	// NoData is true when LocService rendered its "marche pas
	// suffisamment actif" placeholder, i.e. no measurement was
	// available.
	NoData bool `json:"no_data,omitempty"`

	// NoDataMessage carries the literal "marche pas suffisamment actif"
	// sentence when NoData is true. May be empty when the page neither
	// rendered a measurement nor the no-data sentence (treated as a
	// parse failure by the caller).
	NoDataMessage string `json:"no_data_message,omitempty"`

	// URL is the full locservice.fr URL the Source queried. Empty when
	// the Source bailed before building a URL (insufficient inputs).
	URL string `json:"url,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// LocService page carried no measurement (no_data) — the framework
// records Status == StatusOKEmpty in this case.
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return r.SampleSize == 0
}
