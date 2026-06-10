package proptype

import "github.com/bpineau/gazetteer/gazetteer"

// ToListingType maps a raw property-type string onto the coarse
// gazetteer.Listing enum, through Normalize (so every alias the table
// knows — "appartement", "studio", "villa", "local commercial", … —
// resolves). ok is false when the input is unrecognized OR when the
// canonical type has no Listing equivalent (parking, cave, mixed,
// parts, garage): those listings are not residential/land/commercial
// property in the Listing-enum sense, and callers usually want to gate
// them out explicitly rather than run sources with PropertyUnknown
// (which most sources treat as "run anyway").
//
// This is the one bridge between the rich scraping-side vocabulary and
// the four coarse source-gating values — consumers and the CLI share it
// instead of each keeping a private switch.
func ToListingType(raw string) (gazetteer.PropertyType, bool) {
	switch Normalize(raw) {
	case Apartment:
		return gazetteer.PropertyApartment, true
	case House:
		return gazetteer.PropertyHouse, true
	case Land:
		return gazetteer.PropertyLand, true
	case Commercial:
		return gazetteer.PropertyCommercial, true
	default:
		return gazetteer.PropertyUnknown, false
	}
}
