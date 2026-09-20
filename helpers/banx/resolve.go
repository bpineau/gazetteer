package banx

import (
	"context"
	"errors"
	"fmt"
)

// ResolveLatLon resolves a free-form address into (lat, lon) via the
// forward geocoder — the shared tail of the spatial sources' "trust the
// listing's own coords → Geocoder fallback" cascade (cadastre,
// georisques). Callers test the listing's coordinates first
// (gazetteer.Listing.Coords) and call this only on a miss, wrapping the
// returned error with their source prefix.
//
// address is the free-form query line (sources conventionally pass
// "addr zip city" pre-joined); city and zip are the optional
// GeocodeQuery disambiguation hints. A nil geocoder is an error, and so
// is a (0, 0) geocode result — null island is not a French address.
//
// It applies NO precision and NO score floor: whatever BAN matched comes
// back, including the commune centre it answers with for an address that
// does not exist. That is the right answer for a commune-level reading
// and the wrong one for a per-address reading, so the caller decides —
// with ResolveLatLonAt.
func ResolveLatLon(ctx context.Context, g Geocoder, address, city, zip string) (lat, lon float64, err error) {
	lat, lon, _, err = ResolveLatLonAt(ctx, g, address, city, zip, PrecisionUnknown, 0)
	return lat, lon, err
}

// ErrCoarseMatch is returned by ResolveLatLonAt when the geocoder did
// answer, but at a granularity (or with a score) below what the caller
// requires. It means "this address was not found, only its
// surroundings", not "the upstream failed": callers map it onto
// gazetteer.ErrInsufficientInputs or onto a lower-confidence path, never
// onto a retry.
var ErrCoarseMatch = errors.New("banx: geocoder match is coarser than the caller requires")

// ResolveLatLonAt is ResolveLatLon with the two floors a per-address
// reading needs. It also returns the precision it accepted, so the
// caller can record what it read at.
//
// min is the coarsest acceptable granularity (PrecisionUnknown disables
// the check); minScore is the lowest acceptable BAN score (0 disables
// it). A match the geocoder did not label, or did not score, passes
// both: "unreported" is not "coarse", and refusing it would break every
// stub, proxy and hand-built Listing that omits the fields. A refusal
// comes back as ErrCoarseMatch, wrapped with what was asked and what
// arrived.
//
//	// The cadastral parcel under a commune centre is the mairie's.
//	lat, lon, _, err := banx.ResolveLatLonAt(ctx, g, addr, city, zip, banx.PrecisionStreet, 0)
func ResolveLatLonAt(ctx context.Context, g Geocoder, address, city, zip string, min Precision, minScore float64) (lat, lon float64, prec Precision, err error) {
	if g == nil {
		return 0, 0, "", errors.New("lat/lon not resolvable (no geocoder configured)")
	}
	res, err := g.Geocode(ctx, GeocodeQuery{Address: address, City: city, Zip: zip})
	if err != nil {
		return 0, 0, "", err
	}
	if res.Lat == 0 && res.Lon == 0 {
		return 0, 0, "", errors.New("geocoder returned zero coords")
	}
	if res.Precision.CoarserThan(min) {
		return 0, 0, res.Precision, fmt.Errorf("%w: matched %q where %q or finer is required (%q)",
			ErrCoarseMatch, res.Precision, min, res.Label)
	}
	if minScore > 0 && res.Score > 0 && res.Score < minScore {
		return 0, 0, res.Precision, fmt.Errorf("%w: scored %.2f where %.2f is required (%q)",
			ErrCoarseMatch, res.Score, minScore, res.Label)
	}
	return res.Lat, res.Lon, res.Precision, nil
}

// ResolveINSEE resolves the input to a 5-digit INSEE commune code via
// the standard forward/reverse cascade (INSEEResolver, default
// MinForwardScore) — the shared tail of the INSEE-keyed live sources'
// "trust Listing.INSEE → BAN cascade" pattern (bdnb today; dvf's
// resolveINSEE is the same logic and is expected to adopt this helper
// next). When g also implements ReverseGeocoder the reverse step is
// enabled; otherwise the cascade is forward-only.
//
// lat/lon are optional structured coordinates ((0, 0) = absent); when
// both are non-zero they feed the reverse fallback. source identifies
// the resolving step ("ban_forward" | "ban_reverse") for Evidence
// traceability. Callers handle the "listing already carries an INSEE"
// short-circuit themselves and wrap the returned error with their
// source prefix.
func ResolveINSEE(ctx context.Context, g Geocoder, address, city, zip string, lat, lon float64) (insee, source string, err error) {
	if g == nil {
		return "", "", errors.New("no geocoder configured")
	}
	hasText := address != "" || city != "" || zip != ""
	hasCoords := lat != 0 && lon != 0
	if !hasText && !hasCoords {
		return "", "", errors.New("no address/city/zip + no coords")
	}
	var reverse ReverseGeocoder
	if rev, ok := g.(ReverseGeocoder); ok {
		reverse = rev
	}
	resolver := &INSEEResolver{Forward: g, Reverse: reverse}
	res, err := resolver.Resolve(ctx, INSEEQuery{
		Address: address,
		City:    city,
		Zip:     zip,
		Lat:     lat,
		Lon:     lon,
	})
	if err != nil {
		return "", "", err
	}
	if res.INSEE == "" {
		return "", "", errors.New("no INSEE resolved")
	}
	return res.INSEE, res.Source, nil
}
