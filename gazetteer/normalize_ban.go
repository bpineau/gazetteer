package gazetteer

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bpineau/gazetteer/helpers/banx"
	"github.com/bpineau/gazetteer/helpers/communes"
	"github.com/bpineau/gazetteer/helpers/fraddr"
	"github.com/bpineau/gazetteer/helpers/frnorm"
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
	minScore *float64 // nil = DefaultMinGeocodeScore; see WithMinScore
}

// scoreFloor is the score below which a match does not locate the address.
func (n *BANNormalizer) scoreFloor() float64 {
	if n.minScore == nil {
		return DefaultMinGeocodeScore
	}
	return *n.minScore
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

// DefaultMinGeocodeScore is the BAN score below which Normalize does not
// trust a match to locate the address. Calibrated on a small live sample
// (2026-11): a street that does not exist ("3 rue imaginaire qui n'existe
// pas 75011 Paris") came back at housenumber precision with 0.44, while real
// addresses written sloppily scored 0.53 ("12 av J Jaures Montreuil") to
// 0.61 ("106 bd Voltair 75011 Paris"), and clean ones above 0.95. It is a
// floor against the worst guesses, not a proof of a match.
const DefaultMinGeocodeScore = 0.5

// WithMinScore sets the score floor Normalize applies (see Normalize); a
// value <= 0 disables it. Returns the receiver for chaining.
func (n *BANNormalizer) WithMinScore(s float64) *BANNormalizer {
	n.minScore = &s
	return n
}

// Normalize implements gazetteer.Normalizer. Two guards stand between the
// geocoder's best candidate and the Listing, because BAN answers every query
// it can parse with SOMETHING:
//
//   - Département: a postcode written in addr anchors the query (it is
//     passed as GeocodeQuery.Zip, so banx.CachedGeocoder applies its own
//     guard too), and a candidate in another département is refused with
//     ErrAddressNotFound (wrapping banx.ErrDepartmentMismatch): a homonym
//     across the country is not the address.
//
//   - Score: a candidate scored below the floor (DefaultMinGeocodeScore, see
//     WithMinScore) does not locate the address. When it lies in the very
//     postcode addr names, its commune is kept (INSEE, Zip, City) but the
//     coordinates are demoted to PrecisionMunicipality and Address keeps
//     the input text, so per-address Sources refuse it and commune-keyed
//     ones carry on; otherwise it is ErrAddressNotFound.
//
//   - Street type: a candidate whose type de voie contradicts the one addr
//     names ("12 av J Jaures" answered by "12 Place Jean Jaures") is on
//     another street, whatever its score, and is treated as a weak match.
//
// Listing.GeocodeScore carries the score either way.
func (n *BANNormalizer) Normalize(ctx context.Context, addr string) (Listing, error) {
	inputZip, _ := frnorm.ExtractZipFromAddress(addr)
	r, err := n.geocoder.Geocode(ctx, banx.GeocodeQuery{Address: addr, Zip: inputZip})
	if err == nil && inputZip != "" && r.PostCode != "" && !banx.ZipsShareDepartment(inputZip, r.PostCode) {
		err = fmt.Errorf("%w: %s geocoded to postcode %s", banx.ErrDepartmentMismatch, inputZip, r.PostCode)
	}
	lowScore := err == nil && r.Score > 0 && r.Score < n.scoreFloor()
	if lowScore && (inputZip == "" || r.PostCode != inputZip) {
		err = fmt.Errorf("%w: best candidate %q scored %.2f, below %.2f", banx.ErrNotFound, r.Label, r.Score, n.scoreFloor())
	}
	// A weak match keeps its commune but not its position.
	weak := err == nil && (lowScore || streetTypeContradicts(addr, r.Label))
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
		// What the coordinates are the position OF. BAN answers every
		// query it can parse, so a street that does not exist comes back
		// as its commune's centre; carrying the granularity is what lets
		// the per-address Sources refuse it while the commune-keyed ones
		// carry on.
		CoordPrecision: r.Precision,
		GeocodeScore:   r.Score,
	}
	if weak {
		// The commune is right (the candidate is in the postcode addr
		// names), the position within it is not: a different street's
		// number, labelled as if it were the address.
		l.Address = strings.TrimSpace(addr)
		l.CoordPrecision = banx.PrecisionMunicipality
	}
	if n.communes != nil && r.CityCode != "" {
		if c, ok := n.communes.Lookup(r.CityCode); ok {
			l.City = c.Name
		}
	}
	// Resolve the IRIS from the geocoded point, at street precision or finer.
	// A commune or locality centre lies inside some IRIS (usually the
	// mairie's), and attributing it to the address would hand the IRIS-keyed
	// readings (filoiris, logiris, the Paris encadrement quartier) the town
	// hall's neighbourhood. CoordsAtLeast also refuses the (0,0) sentinel.
	if _, _, ok := l.CoordsAtLeast(banx.PrecisionStreet); n.iris != nil && ok {
		if code, ok := n.iris.ResolveIRIS(lat, lon); ok {
			l.IRIS = code
		}
	}
	return l, nil
}

// streetTypeContradicts reports whether addr and label both name a type de
// voie and the two differ: the candidate is then another street of the
// commune (an avenue answered by a place of the same name).
func streetTypeContradicts(addr, label string) bool {
	in, out := fraddr.StreetOf(addr), fraddr.StreetOf(label)
	return in.Type != "" && out.Type != "" && in.Type != out.Type
}
