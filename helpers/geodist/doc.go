// Package geodist computes great-circle (haversine) distances
// between (lat, lon) points on Earth.
//
// Earth radius: R = 6371.0 km — the IUGG mean radius. The package
// is a thin numerical kernel; it allocates nothing per call and is
// safe for concurrent use.
//
// Example:
//
//	d := geodist.KmBetween(48.8566, 2.3522, 45.7640, 4.8357)
//	// d ≈ 391.5 (Paris ↔ Lyon, km)
package geodist
