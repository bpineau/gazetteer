package qpv

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers can match on them without importing this
// package's constants.
const (
	// ConfidenceHigh is returned when the answer is derived from
	// point-in-polygon (coordinates were available): the address is
	// definitively inside, or definitively outside, every QPV polygon.
	ConfidenceHigh = "high"

	// ConfidenceMedium is returned by the commune-level fallback used
	// when the Listing carries no coordinates. The commune is known to
	// host QPVs but the Source cannot tell whether THIS address sits in
	// one — it returns the whole commune's list at lower confidence.
	ConfidenceMedium = "medium"

	// ConfidenceNone is the zero value — no QPV signal (a point outside
	// all polygons, or a coordinate-less query on a QPV-free commune).
	ConfidenceNone = ""
)

// Match levels reported in Result.MatchLevel — how the answer was
// derived. Stable strings.
const (
	// MatchLevelPoint means the answer came from point-in-polygon over
	// the listing's coordinates: HasQPV is the real "is THIS address in
	// a QPV?" answer.
	MatchLevelPoint = "point"

	// MatchLevelCommune means the answer is the commune-level fallback
	// (no coordinates): HasQPV reflects "does this commune host any
	// QPV?", NOT whether this address is inside one.
	MatchLevelCommune = "commune"
)

// QPV is one Quartier Prioritaire — a code (e.g. "QP075002") and a
// human-readable label.
type QPV struct {
	// Code is the official ANCT QPV identifier from the 2024 contours,
	// format "QPXXXNNN" where XXX is the department code and NNN an
	// order within the department (e.g. "QP075002").
	Code string `json:"code"`

	// Label is the QPV's official name (e.g. "La Goutte d'Or",
	// "La Duchère").
	Label string `json:"label,omitempty"`
}

// Result is the typed payload returned by Source.Query.
//
// The semantics of HasQPV depend on MatchLevel:
//
//   - MatchLevel == "point" (coordinates were available): HasQPV
//     answers the real question — "is THIS address inside a QPV
//     polygon?". When true, QPVs holds exactly the containing QPV.
//   - MatchLevel == "commune" (no coordinates): HasQPV answers the
//     weaker "does this commune host any QPV?" and QPVs lists every QPV
//     in the commune. Confidence is then ConfidenceMedium.
type Result struct {
	// HasQPV is true when the listing is inside a QPV (point match) or,
	// on the commune fallback, when the commune hosts at least one QPV.
	HasQPV bool `json:"has_qpv"`

	// QPVCount is the number of QPVs in QPVs. For a point match it is 0
	// or 1; for the commune fallback it is the commune's QPV count.
	QPVCount int `json:"qpv_count,omitempty"`

	// QPVs lists the relevant QPVs (code + label). For a point match
	// this is the single containing QPV; for the commune fallback it is
	// every QPV in the commune.
	QPVs []QPV `json:"qpvs,omitempty"`

	// MatchLevel records how the answer was derived: MatchLevelPoint
	// (point-in-polygon, authoritative) or MatchLevelCommune (the
	// coordinate-less fallback). Empty when no answer was produced.
	MatchLevel string `json:"match_level,omitempty"`

	// NearestCode is the code of the closest QPV when the point match
	// found the address OUTSIDE all polygons but a QPV lies within
	// NearestQPVMaxMeters. Empty otherwise. A hint only — it never
	// affects HasQPV.
	NearestCode string `json:"nearest_code,omitempty"`

	// NearestLabel is the label of the QPV identified by NearestCode.
	NearestLabel string `json:"nearest_label,omitempty"`

	// NearestMeters is the great-circle distance, in metres, from the
	// listing to the nearest vertex of the QPV identified by
	// NearestCode. Zero when no nearby QPV was found.
	NearestMeters float64 `json:"nearest_meters,omitempty"`

	// Confidence is ConfidenceHigh for a point match (inside or
	// outside), ConfidenceMedium for the commune fallback, and
	// ConfidenceNone when no signal was produced.
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
	// INSEE is the 5-digit commune code the Source filtered on (after
	// arrondissement folding on the commune fallback path).
	INSEE string `json:"insee"`

	// CommuneLabel is the human-readable commune name. Useful for
	// logging / diagnostics; not always populated.
	CommuneLabel string `json:"commune_label,omitempty"`

	// Lat / Lon are the coordinates used for the point-in-polygon test
	// (decimal degrees). Zero on the commune fallback path.
	Lat float64 `json:"lat,omitempty"`
	Lon float64 `json:"lon,omitempty"`

	// PolygonCount is the total number of QPV polygons in the index.
	// Sanity scalar for downstream renderers.
	PolygonCount int `json:"polygon_count,omitempty"`

	// RowCountCommunes is the total number of communes hosting at least
	// one QPV (the commune fallback index size).
	RowCountCommunes int `json:"row_count_communes,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter. Returns true when the
// Source produced no positive QPV signal — the framework records
// Status == StatusOKEmpty in this case.
//
// A point match that lands OUTSIDE all QPV (the correct answer for most
// addresses) is IsEmpty() == true even though it is a high-confidence
// answer; that mirrors every other spatial source (an out-of-polygon
// hit is "no data for this address").
func (r *Result) IsEmpty() bool {
	if r == nil {
		return true
	}
	return !r.HasQPV
}
