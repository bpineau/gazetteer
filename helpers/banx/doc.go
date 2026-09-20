// Package banx is a small, opinionated BAN (Base Adresse Nationale)
// client. It packages:
//
//   - Forward geocoding (free-form FR address → lat/lon + INSEE)
//   - Reverse geocoding (lat/lon → INSEE + canonical label)
//   - A persistent cache layer on top of any kvcache.Cache
//   - An INSEEResolver cascade (forward, then reverse on coords) for
//     callers whose input may carry either text or coordinates.
//   - The match GRANULARITY (Precision), and the floors a per-address
//     reading needs (ResolveLatLonAt). BAN answers every query it can
//     parse, so a street that does not exist comes back as its commune's
//     centre: a real coordinate, with a real parcel and a real nearest
//     station, and nothing but the type to say the address was not found.
//
// Package layout:
//
//	geocode.go         Geocoder/ReverseGeocoder interfaces + result type.
//	ban.go             BANClient that hits api-adresse.data.gouv.fr.
//	cache.go           CachedGeocoder + coherence guard.
//	insee_resolver.go  INSEEResolver cascade.
//	precision.go       Precision: what a coordinate is the position OF.
//	resolve.go         ResolveLatLon / ResolveLatLonAt / ResolveINSEE.
//
// Designed to be the smallest reusable piece for any French
// real-estate / mobility / public-data app: a BANClient implements
// both Geocoder and ReverseGeocoder, the CachedGeocoder wraps it, and
// the INSEEResolver runs the canonical "forward first, reverse as a
// fallback if coordinates are known" cascade.
package banx
