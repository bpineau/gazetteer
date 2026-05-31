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
//   - This is the full haversine formula on a spherical Earth; its
//     only error is the sphere-vs-ellipsoid mismatch, at most ~0.5 %
//     relative (good enough for property-to-property comparisons
//     within a city).
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
