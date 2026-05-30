// Package gpe is a gazetteer.Source that locates the nearest future Grand
// Paris Express station to a listing — the new metro network (lines 14
// extensions, 15, 16, 17, 18; ~68 new stations) opening across
// Île-de-France. The embedded catalog is the Société du Grand Paris
// station-points dataset verbatim (its exact count is Meta.StationCount).
//
// Proximity to a future GPE station is a major rental-demand and
// capital-appreciation driver for an IDF investor whose thesis is "near a
// station, not Paris". The Source answers the accurate, durable question —
// which station, which line, how far — and deliberately does NOT assert an
// opening year: the published calendar shifts and a single line label (e.g.
// "L15") spans sections opening years apart, so a per-station date would be
// guesswork. Look up the section's phase from the line.
//
// Fully offline: the station catalog (Société du Grand Paris) ships
// embedded under `data/`. Spatial — needs the listing's coordinates.
package gpe

// Confidence values returned in Result.Confidence.
const (
	ConfidenceHigh = "high"
	ConfidenceNone = ""
)

// Station is one future GPE station near the listing.
type Station struct {
	// Code is the SGP station code (e.g. "GA26").
	Code string `json:"code"`

	// Name is the station name (e.g. "Nanterre La Boule").
	Name string `json:"name"`

	// Line is the GPE line label, verbatim — a single line ("L15") or, at an
	// interchange, several ("L16/L17").
	Line string `json:"line"`

	// DistanceM is the haversine distance from the listing, in metres.
	DistanceM int `json:"distance_m"`
}

// Result is the typed payload returned by Source.Query.
type Result struct {
	// Nearest is the closest future GPE station, or nil when none is within
	// MaxRelevantMeters.
	Nearest *Station `json:"nearest,omitempty"`

	// Within1500m / Within3000m count the future GPE stations within those
	// radii — a walkable station vs a broader catchment.
	Within1500m int `json:"within_1500m"`
	Within3000m int `json:"within_3000m"`

	// Confidence is "high" when a station is in range, ConfidenceNone
	// otherwise.
	Confidence string `json:"confidence,omitempty"`

	// Evidence captures reproducibility metadata. Sidecar — not wire data.
	Evidence Evidence `json:"-"`
}

// Evidence captures reproducibility metadata about the query.
type Evidence struct {
	// Lat / Lon are the listing coordinates the search anchored on.
	Lat float64 `json:"lat"`
	Lon float64 `json:"lon"`

	// StationCount is the size of the embedded catalog.
	StationCount int `json:"station_count,omitempty"`
}

// IsEmpty satisfies gazetteer.EmptyReporter: true when no future GPE station
// is within MaxRelevantMeters of the listing.
func (r *Result) IsEmpty() bool {
	return r == nil || r.Nearest == nil
}
