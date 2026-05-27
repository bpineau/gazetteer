// Package proptype owns the single source of truth that maps raw
// property-type strings (any case, any language, possibly with
// trailing whitespace) onto the canonical enum used by Sources to
// gate eligibility.
//
// The canonical values match the gazetteer.PropertyType constants:
// PropertyApartment, PropertyHouse, PropertyLand, PropertyCommercial,
// plus PropertyUnknown as the catch-all.
//
// Callers writing the column should still emit the canonical
// English-ish slug (`apartment`, `house`, …) the package's
// PropertyType.String() returns.
//
// Example:
//
//	pt := proptype.Parse("Appartement")
//	if pt == proptype.Apartment {
//	    // ...
//	}
package proptype
