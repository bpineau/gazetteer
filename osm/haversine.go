package osm

import (
	"math"

	"github.com/bpineau/gazetteer/pkg/geodist"
)

// EarthRadiusMeters — deprecated, kept for back-compat with callers
// that did `EarthRadiusMeters * c`. New code should use
// `geodist.EarthRadiusKm * 1000`. Value byte-identical (6 371 000 m).
const EarthRadiusMeters = geodist.EarthRadiusKm * 1000

// WalkSinuosityMultiplier scales straight-line (haversine) distance up
// to "approximate distance walked on a real street grid". 1.3 is the
// figure published by the OpenStreetMap routing community for dense
// urban areas (Paris, Lyon — where most of our stations live) ; rural
// France is closer to 1.4 but the operator wants one homogeneous
// estimate, not a per-row mode switch.
const WalkSinuosityMultiplier = 1.3

// WalkSpeedMetersPerMinute is the canonical adult walking speed used to
// convert metres → minutes at render time. 80 m/min ≈ 4.8 km/h is the
// World Health Organization "normal" pace for urban pedestrians.
const WalkSpeedMetersPerMinute = 80.0

// HaversineMeters returns the great-circle distance between two
// (lat, lon) pairs in metres. Thin alias on [geodist.MetersBetween]
// kept for the existing call sites in this package.
func HaversineMeters(lat1, lon1, lat2, lon2 float64) float64 {
	return geodist.MetersBetween(lat1, lon1, lat2, lon2)
}

// WalkingMetersFromHaversine returns the haversine distance scaled up
// by the urban sinuosity multiplier. Rounds to the nearest metre — the
// downstream consumer is a UI label ("~8 min à pied"), sub-metre
// precision is meaningless.
func WalkingMetersFromHaversine(haversineMeters float64) int {
	v := haversineMeters * WalkSinuosityMultiplier
	if v < 0 {
		return 0
	}
	return int(math.Round(v))
}

// WalkMinutes converts metres-walked to integer minutes at the
// canonical walking speed. Floors below 1 minute to 1 so the UI never
// renders "0 min à pied" — the floor is more honest at MVP precision
// than a fake fractional value.
func WalkMinutes(walkMeters int) int {
	m := float64(walkMeters) / WalkSpeedMetersPerMinute
	if m < 1 {
		return 1
	}
	return int(math.Round(m))
}
