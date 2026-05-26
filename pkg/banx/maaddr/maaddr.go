package maaddr

import (
	"context"
	"strings"

	"github.com/bpineau/gazetteer/pkg/banx"
)

// CanonicalizeAddress asks BAN for the canonical form of a raw address
// and returns the street-only portion (BAN includes the zip and city
// in its label, which we strip — the autocomplete-shaped caller already
// has them via the property's city/zip context).
//
// Returns (normalized, true) ONLY when:
//   - BAN returned a non-empty label, AND
//   - the stripped result differs from the raw address (a no-op
//     normalization gives the same downstream result, so we'd just burn a
//     2nd autocomplete call for nothing).
//
// Any geocoder error (including banx.ErrNotFound) maps to ("", false).
//
// Shared between the in-tree MeilleursAgents enricher and the
// web-handler queue path so the BAN-normalized retry recipe (strip +
// no-op-guard) lives in one place. See memory
// `matcher_cluster_online_symmetry`.
func CanonicalizeAddress(ctx context.Context, g banx.Geocoder, address, city, zip string) (string, bool) {
	if g == nil {
		return "", false
	}
	res, err := g.Geocode(ctx, banx.GeocodeQuery{
		Address: address,
		City:    city,
		Zip:     zip,
	})
	if err != nil || res.Label == "" {
		return "", false
	}
	norm := StripTrailingZipCity(res.Label, res.PostCode)
	norm = strings.TrimSpace(norm)
	if norm == "" || norm == address {
		return "", false
	}
	return norm, true
}

// StripTrailingZipCity trims a trailing "<zip> <city>" tail from a BAN
// label so the resulting string is just the street + house number, which
// is what an autocomplete `q=` parameter typically expects (the city/zip
// are passed separately via the URL components).
//
// Example: "33 boulevard du Château 92210 Saint-Cloud" → "33 boulevard du Château".
//
// When the zip is missing from the label OR the zip argument is empty,
// the label is returned unchanged. The strip is non-destructive on
// labels BAN formats without the trailing zip+city (rare but possible).
func StripTrailingZipCity(label, zip string) string {
	label = strings.TrimSpace(label)
	zip = strings.TrimSpace(zip)
	if label == "" {
		return ""
	}
	if zip != "" {
		if i := strings.LastIndex(label, zip); i >= 0 {
			return strings.TrimRight(strings.TrimSpace(label[:i]), " ,")
		}
	}
	return label
}
