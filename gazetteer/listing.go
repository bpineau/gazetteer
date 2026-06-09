package gazetteer

import "time"

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
