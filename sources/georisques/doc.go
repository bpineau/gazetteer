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
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := georisques.NewSource(georisques.Options{
//	    BaseURL:  srv.URL,
//	    Geocoder: ban,
//	})
//	lat, lon := 48.85, 2.35
//	data, err := src.Query(ctx, gazetteer.Listing{Lat: &lat, Lon: &lon})
//	if err != nil { log.Fatal(err) }
//	r := data.(*georisques.Result)
//	if r.IsEmpty() {
//	    fmt.Println("BRGM returned no risk data for these coords")
//	    return
//	}
//	fmt.Printf("%d natural hazards, %d technological hazards\n",
//	    r.Summary.NaturelsPresentCount, r.Summary.TechnosPresentCount)
//	for _, flag := range r.Summary.RedFlags {
//	    fmt.Println("  red flag:", flag)
//	}
//	if r.ReportURL != "" {
//	    fmt.Println("full report:", r.ReportURL)
//	}
package georisques
