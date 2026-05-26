// Package banx is a small, opinionated BAN (Base Adresse Nationale)
// client. It packages:
//
//   - Forward geocoding (free-form FR address → lat/lon + INSEE)
//   - Reverse geocoding (lat/lon → INSEE + canonical label)
//   - A persistent cache layer on top of any kvcache.Cache
//   - An INSEEResolver cascade (forward, then reverse on coords) for
//     callers whose input may carry either text or coordinates.
//
// Package layout:
//
//	geocode.go         Geocoder/ReverseGeocoder interfaces + result type.
//	ban.go             BANClient that hits api-adresse.data.gouv.fr.
//	cache.go           CachedGeocoder + coherence guard.
//	insee_resolver.go  INSEEResolver cascade.
//
// Designed to be the smallest reusable piece for any French
// real-estate / mobility / public-data app: a BANClient implements
// both Geocoder and ReverseGeocoder, the CachedGeocoder wraps it, and
// the INSEEResolver runs the canonical "forward first, reverse as a
// fallback if coordinates are known" cascade.
package banx
