// Package georisques is a gazetteer.Source for the BRGM
// rapport-risque endpoint hosted at georisques.gouv.fr — natural and
// technological hazards at the (lat, lon) granularity.
//
// The Source resolves the listing's lat/lon (preferring the
// Listing's pointers; falling back to a banx.Geocoder when
// configured), hits the rapport-risque endpoint, flattens the
// response and returns a *Result. The typed Result satisfies
// appraisal.HazardReporter so it contributes to
// appraisal.HazardProfile.
//
// Empty parses (Adresse + Commune both empty) surface as
// IsEmpty()==true.
//
// Example:
//
//	src := georisques.NewSource(georisques.Options{
//	    BaseURL:  srv.URL,
//	    Geocoder: ban,
//	})
//	r, err := src.Query(ctx, gazetteer.Listing{
//	    Lat: floatPtr(48.85), Lon: floatPtr(2.35),
//	})
package georisques
