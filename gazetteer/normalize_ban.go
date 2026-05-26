package gazetteer

import (
	"context"

	"github.com/bpineau/gazetteer/helpers/banx"
	"github.com/bpineau/gazetteer/helpers/communes"
)

// BANNormalizer canonicalises a free-text French address into a Listing
// by delegating to a banx.Geocoder and (optionally) looking up the
// commune name via communes.Communes to populate Listing.City.
//
// The result populates Address (canonical Label from BAN), Zip (PostCode),
// INSEE (CityCode), Lat, Lon. If communes is nil, Listing.City is left empty.
type BANNormalizer struct {
	geocoder banx.Geocoder
	communes communes.Communes
}

// NewBANNormalizer wires a Geocoder (required) and a Communes lookup
// (optional, nil disables city population) into a Normalizer. In production,
// pass a *banx.BANClient constructed via banx.NewBANClient with an
// httpx.Client and communes.MustDefault() (or any communes.Communes); in tests,
// pass any banx.Geocoder stub and nil for communes.
func NewBANNormalizer(g banx.Geocoder, c communes.Communes) *BANNormalizer {
	return &BANNormalizer{geocoder: g, communes: c}
}

// Normalize implements gazetteer.Normalizer.
func (n *BANNormalizer) Normalize(ctx context.Context, addr string) (Listing, error) {
	r, err := n.geocoder.Geocode(ctx, banx.GeocodeQuery{Address: addr})
	if err != nil {
		return Listing{}, err
	}
	lat, lon := r.Lat, r.Lon
	l := Listing{
		Address: r.Label,
		Zip:     r.PostCode,
		INSEE:   r.CityCode,
		Lat:     &lat,
		Lon:     &lon,
	}
	if n.communes != nil && r.CityCode != "" {
		if c, ok := n.communes.Lookup(r.CityCode); ok {
			l.City = c.Name
		}
	}
	return l, nil
}
