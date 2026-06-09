package banx

import (
	"context"
	"errors"
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
func ResolveLatLon(ctx context.Context, g Geocoder, address, city, zip string) (lat, lon float64, err error) {
	if g == nil {
		return 0, 0, errors.New("lat/lon not resolvable (no geocoder configured)")
	}
	res, err := g.Geocode(ctx, GeocodeQuery{Address: address, City: city, Zip: zip})
	if err != nil {
		return 0, 0, err
	}
	if res.Lat == 0 && res.Lon == 0 {
		return 0, 0, errors.New("geocoder returned zero coords")
	}
	return res.Lat, res.Lon, nil
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
