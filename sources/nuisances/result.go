package nuisances

// Confidence value returned in Result.Confidence. A resolved cell is an exact
// grid match, so a populated reading is high confidence.
const ConfidenceHigh = "high"

// Tier classifies the cumulative nuisance count. Stable strings so downstream
// consumers can match without importing this package.
const (
	TierCalme      = "calme"       // 0 overlapping nuisances
	TierModere     = "modéré"      // 1
	TierExpose     = "exposé"      // 2
	TierTresExpose = "très exposé" // 3–4
)

// Result is the typed payload returned by Source.Query: the listing cell's
// cumulative environmental-nuisance exposure.
type Result struct {
	// NuisanceCount is the number of overlapping environmental nuisances on the
	// cell (0–4): road/rail/air-traffic noise and air pollution.
	NuisanceCount int `json:"nuisance_count"`

	// PointNoir flags a "point noir environnemental" — a cell where several
	// nuisances saturate, the worst cadre-de-vie spots.
	PointNoir bool `json:"point_noir"`

	// Tier is the human-readable exposure tier (see Tier* constants).
	Tier string `json:"tier,omitempty"`

	// Confidence is "high" on a resolved cell, empty otherwise.
	Confidence string `json:"confidence,omitempty"`

	// Evidence captures reproducibility metadata. Sidecar — not wire data.
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query.
type Evidence struct {
	// ListingLat / ListingLon are the input coordinates.
	ListingLat float64 `json:"listing_lat"`
	ListingLon float64 `json:"listing_lon"`

	// CellDistanceM is the distance to the resolved cell centre, in metres.
	CellDistanceM int `json:"cell_distance_m"`

	// GridCells is the number of cells the snapshot holds.
	GridCells int `json:"grid_cells"`
}

// IsEmpty satisfies gazetteer.EmptyReporter: true when no grid cell covers the
// listing (outside the Île-de-France perimeter). A resolved calm cell
// (NuisanceCount 0) is NOT empty — "no nuisance here" is a real, useful reading.
func (r *Result) IsEmpty() bool {
	return r == nil || r.Tier == ""
}

// tierFor maps a nuisance count to its exposure tier.
func tierFor(count int) string {
	switch {
	case count >= 3:
		return TierTresExpose
	case count == 2:
		return TierExpose
	case count == 1:
		return TierModere
	default:
		return TierCalme
	}
}
