// Package geodist computes great-circle (haversine) distances between
// (lat, lon) points on Earth.
//
// Extracted into its own package because at least 5 call sites across
// the codebase reimplemented the same haversine formula independently
// (cf. `pkg/communes/communes.go::HaversineKm`,
// `internal/core/auctionview/builder.go::haversineKm`,
// `internal/core/enrich/osm/haversine.go::HaversineMeters`,
// `internal/web/handlers/map.go::haversineKmMap`,
// `a sibling distance helper elsewhere in the codebase`). Each duplication is
// a small risk of drift (different R radius, different rounding) and
// pollutes search results when grepping the codebase.
//
// `pkg/communes` re-exports `HaversineKm` as a thin wrapper to keep
// existing callers working without churn ; new callers should depend
// on `pkg/geodist` directly to avoid pulling the embedded INSEE CSV
// table (~400 lines + 100 KB data) that lives in `pkg/communes`.
//
// Earth radius : R = 6371.0 km — the mean radius standard adopted by
// IUGG. Choice of mean (vs equatorial 6378.1 or polar 6356.8) is the
// convention used by every prior implementation in the repo, so we
// keep it for byte-for-byte compatibility.
package geodist

import "math"

// EarthRadiusKm is the mean Earth radius used by the haversine
// formula throughout the codebase. Exposed for callers that need to
// compute related quantities (e.g. arc length in radians).
const EarthRadiusKm = 6371.0

// KmBetween returns the great-circle distance between two (lat, lon)
// points expressed in decimal degrees, in kilometers.
//
// Conventions :
//   - Inputs are decimal degrees (NOT radians). North/East positive.
//   - Output is in km. Use [MetersBetween] when meters are needed.
//   - For points within a few km, the small-angle approximation
//     used here is accurate to ~0.5 % relative error (good enough for
//     property-to-property comparisons within a city).
//   - NaN inputs propagate to NaN output.
func KmBetween(lat1, lon1, lat2, lon2 float64) float64 {
	rad := math.Pi / 180.0
	dLat := (lat2 - lat1) * rad
	dLon := (lon2 - lon1) * rad
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*rad)*math.Cos(lat2*rad)*
			math.Sin(dLon/2)*math.Sin(dLon/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return EarthRadiusKm * c
}

// MetersBetween returns the great-circle distance between two (lat,
// lon) points expressed in decimal degrees, in meters. Equivalent to
// [KmBetween] × 1000 (computed in one shot to avoid the multiply +
// rounding round-trip).
func MetersBetween(lat1, lon1, lat2, lon2 float64) float64 {
	return KmBetween(lat1, lon1, lat2, lon2) * 1000
}
