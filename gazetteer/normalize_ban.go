package gazetteer

import (
	"context"
	"errors"
	"fmt"

	"github.com/bpineau/gazetteer/helpers/banx"
	"github.com/bpineau/gazetteer/helpers/communes"
)

// IRISResolver maps a coordinate to its 9-digit INSEE IRIS code. The core
// depends on this interface, not on any concrete IRIS dataset — dependency
// inversion, mirroring banx.Geocoder. sources/iris provides an implementation.
type IRISResolver interface {
	// ResolveIRIS returns the IRIS code containing (lat, lon). ok is false when
	// the point is outside the resolver's coverage.
	ResolveIRIS(lat, lon float64) (code string, ok bool)
}

// BANNormalizer canonicalises a free-text French address into a Listing
// by delegating to a banx.Geocoder and (optionally) looking up the
// commune name via communes.Communes to populate Listing.City, and the IRIS
// code via an IRISResolver.
//
// The result populates Address (canonical Label from BAN), Zip (PostCode),
// INSEE (CityCode), Lat, Lon. If communes is nil, Listing.City is left empty;
// if no IRISResolver is wired, Listing.IRIS is left empty.
type BANNormalizer struct {
	geocoder banx.Geocoder
	communes communes.Communes
	iris     IRISResolver
}

// NewBANNormalizer wires a Geocoder (required) and a Communes lookup
// (optional, nil disables city population) into a Normalizer. In production,
// pass a *banx.BANClient constructed via banx.NewBANClient with an
// httpx.Client and communes.MustDefault() (or any communes.Communes); in tests,
// pass any banx.Geocoder stub and nil for communes.
//
// Chain WithIRIS to also populate Listing.IRIS.
func NewBANNormalizer(g banx.Geocoder, c communes.Communes) *BANNormalizer {
	return &BANNormalizer{geocoder: g, communes: c}
}

// WithIRIS installs an IRISResolver so Normalize populates Listing.IRIS from the
// geocoded coordinates. Returns the receiver for chaining. A nil resolver leaves
// IRIS resolution disabled.
func (n *BANNormalizer) WithIRIS(r IRISResolver) *BANNormalizer {
	n.iris = r
	return n
}

// Normalize implements gazetteer.Normalizer.
func (n *BANNormalizer) Normalize(ctx context.Context, addr string) (Listing, error) {
	r, err := n.geocoder.Geocode(ctx, banx.GeocodeQuery{Address: addr})
	if err != nil {
		// Raise the "no such address" cases to the core taxonomy so consumers
		// classify on gazetteer.ErrAddressNotFound instead of importing banx.
		// Double-%w keeps errors.Is matching the original banx sentinels too.
		if errors.Is(err, banx.ErrNotFound) || errors.Is(err, banx.ErrDepartmentMismatch) {
			return Listing{}, fmt.Errorf("%w: %w", ErrAddressNotFound, err)
		}
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
	// Resolve the IRIS from the geocoded point. (0,0) is the "unset coords"
	// sentinel — a real address never geocodes to Null Island.
	if n.iris != nil && !(lat == 0 && lon == 0) {
		if code, ok := n.iris.ResolveIRIS(lat, lon); ok {
			l.IRIS = code
		}
	}
	return l, nil
}
