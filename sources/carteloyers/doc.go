// Package carteloyers is a gazetteer.Source for the ANIL / DHUP
// "carte des loyers" offline dataset — a per-commune × typology
// reference rent in EUR/m²/month.
//
// The Source picks a typology bucket (House, Apt 1-2 pieces, Apt 3+,
// generic Apt) from the listing's property type + rooms, looks it up
// in the embedded JSON index, and returns a *Result that satisfies
// appraisal.RentEstimator. When the rooms-bucket dataset misses on a
// commune the Source falls back to the generic apartment dataset and
// stamps Evidence.FallbackToGeneric = true.
//
// Example:
//
//	src := carteloyers.NewSource(carteloyers.Options{})
//	r, err := src.Query(ctx, gazetteer.Listing{
//	    INSEE:        "75101",
//	    PropertyType: gazetteer.PropertyApartment,
//	    Rooms:        intPtr(3),
//	})
package carteloyers
