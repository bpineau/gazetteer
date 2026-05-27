// Package encadrement is a gazetteer.Source for the French zoned
// rent caps ("encadrement des loyers") in force in Paris, Plaine
// Commune and Lyon / Villeurbanne.
//
// The Source matches the listing to a zone:
//
//   - Paris by zip (75001..75020, 75116)
//   - Lyon / Villeurbanne by INSEE (69381..69389, 69266)
//   - Plaine Commune currently returns ConfidenceNone (no INSEE → zone
//     map yet)
//
// then collapses the cells matching (pieces, non-meublé, non-maison)
// by median of LoyerRefMaxEURPerM2HC. The *Result satisfies
// appraisal.RentEstimator with Bracket populated, so consumers can
// label the rent as a "loyer de référence" rather than a market
// estimate.
//
// Example:
//
//	src := encadrement.NewSource(encadrement.Options{})
//	r, err := src.Query(ctx, gazetteer.Listing{
//	    Zip:          "75001",
//	    PropertyType: gazetteer.PropertyApartment,
//	    Rooms:        intPtr(3),
//	    SurfaceM2:    floatPtr(50),
//	})
package encadrement
