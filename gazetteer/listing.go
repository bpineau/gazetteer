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

	PropertyType PropertyType `json:"property_type,omitempty"`
	SurfaceM2    *float64     `json:"surface_m2,omitempty"`
	Rooms        *int         `json:"rooms,omitempty"`
	BuildYear    *int         `json:"build_year,omitempty"`

	// AsOf is the reference date for time-sensitive lookups (DVF
	// window, encadrement zones, taxe foncière). Zero means "as of now".
	AsOf time.Time `json:"as_of,omitzero"`
}

// PropertyType is a coarse, source-agnostic classification used to gate
// per-source eligibility (e.g. DVF skips parking lots, BienIci skips
// commercial).
type PropertyType string

const (
	PropertyUnknown    PropertyType = ""
	PropertyApartment  PropertyType = "apartment"
	PropertyHouse      PropertyType = "house"
	PropertyLand       PropertyType = "land"
	PropertyCommercial PropertyType = "commercial"
)
