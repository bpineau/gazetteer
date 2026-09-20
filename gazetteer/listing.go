package gazetteer

import (
	"time"

	"github.com/bpineau/gazetteer/helpers/banx"
)

// Listing is the universal input to every Source. Address attributes may
// be unknown (zero string / nil pointer); each Source decides whether the
// fields it needs are present and returns ErrInsufficientInputs if not.
//
// Optional numeric fields use pointers so absent (nil) is unambiguous;
// 0.0 is a legal value for Lat, SurfaceM2, etc.
type Listing struct {
	Address string   `json:"address,omitempty"`
	City    string   `json:"city,omitempty"`
	Zip     string   `json:"zip,omitempty"`
	INSEE   string   `json:"insee,omitempty"`
	Lat     *float64 `json:"lat,omitempty"`
	Lon     *float64 `json:"lon,omitempty"`

	// IRIS is the 9-digit INSEE IRIS code (sub-commune statistical zone, e.g.
	// "751104201"), populated by a Normalizer that has an IRISResolver. Empty
	// when unresolved (outside the resolver's coverage, or no resolver wired).
	// Sources keyed at IRIS granularity read it; commune-level sources ignore it.
	IRIS string `json:"iris,omitempty"`

	// CoordPrecision is what Lat/Lon are the position OF: the address
	// itself, its street, its hamlet or its commune's centre. A
	// Normalizer fills it from the geocoder; a hand-built Listing leaves
	// it empty, which means "unreported", NOT "coarse".
	//
	// It exists because a commune centre is a perfectly ordinary
	// coordinate: it has a cadastral parcel, a DPE and a nearest metro
	// station, so a per-address reading taken on one is wrong with no
	// outward sign. Sources that read at address granularity gate on it
	// via CoordsAtLeast; commune-keyed sources ignore it.
	CoordPrecision banx.Precision `json:"coord_precision,omitempty"`

	PropertyType PropertyType `json:"property_type,omitempty"`
	SurfaceM2    *float64     `json:"surface_m2,omitempty"`
	Rooms        *int         `json:"rooms,omitempty"`
	BuildYear    *int         `json:"build_year,omitempty"`

	// AsOf is the reference date for time-sensitive lookups (DVF
	// window, encadrement zones, taxe foncière). Zero means "as of now".
	AsOf time.Time `json:"as_of,omitzero"`
}

// Coords returns the listing's coordinates when both are present and not
// the (0, 0) null-island placeholder, which no French address resolves
// to. This is the canonical "does the listing carry usable coordinates"
// test — spatial sources and renderers should use it rather than
// hand-checking the Lat/Lon pointers.
func (l Listing) Coords() (lat, lon float64, ok bool) {
	if l.Lat == nil || l.Lon == nil {
		return 0, 0, false
	}
	if *l.Lat == 0 && *l.Lon == 0 {
		return 0, 0, false
	}
	return *l.Lat, *l.Lon, true
}

// CoordsAtLeast is Coords with a precision floor: ok is false when the
// listing's coordinates are known to be coarser than min, so a Source
// reading at address granularity can refuse them, or fall back to its
// commune-level path, instead of reading a commune centre as if it were
// a doorstep.
//
// A listing whose CoordPrecision is empty passes: "the geocoder did not
// say" is not "too coarse", and refusing it would break every Listing
// assembled by hand or by a normalizer that reports no precision. See
// banx.Precision for what each granularity can be read for.
//
//	// A QPV contour is a neighbourhood: a street centroid is inside it
//	// or outside it, a commune centre says nothing.
//	lat, lon, ok := l.CoordsAtLeast(banx.PrecisionStreet)
func (l Listing) CoordsAtLeast(min banx.Precision) (lat, lon float64, ok bool) {
	lat, lon, ok = l.Coords()
	if !ok {
		return 0, 0, false
	}
	if l.CoordPrecision.CoarserThan(min) {
		return 0, 0, false
	}
	return lat, lon, true
}

// PropertyType is a coarse, source-agnostic classification used to gate
// per-source eligibility (e.g. DVF skips parking lots; residential-only
// Sources skip commercial).
type PropertyType string

const (
	PropertyUnknown    PropertyType = ""
	PropertyApartment  PropertyType = "apartment"
	PropertyHouse      PropertyType = "house"
	PropertyLand       PropertyType = "land"
	PropertyCommercial PropertyType = "commercial"
)
