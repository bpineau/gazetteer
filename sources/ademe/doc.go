// Package ademe is a gazetteer.Source for the ADEME `dpe03existant`
// dataset (energy-performance certificates) published via
// data.gouv.fr's data-fair API.
//
// The Source resolves the listing's zip via a banx.Geocoder when
// missing, hits the ADEME endpoint with an address pattern derived
// from fraddr.Parse, picks the best candidate by (label score,
// recency) and returns a *Result carrying DPE label, GES label,
// surface, build year and dwelling type.
//
// Example:
//
//	src := ademe.NewSource(ademe.Options{
//	    BaseURL:  srv.URL,           // optional, defaults to DefaultBaseURL
//	    Geocoder: ban,               // banx.Geocoder
//	})
//	r, err := src.Query(ctx, gazetteer.Listing{
//	    Address: "1 rue de Rivoli",
//	    Zip:     "75001",
//	})
//
// Empty responses (results: []) surface as IsEmpty()==true; the
// framework records StatusOKEmpty.
package ademe
