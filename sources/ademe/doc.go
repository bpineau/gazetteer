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
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := ademe.NewSource(ademe.Options{
//	    BaseURL:  srv.URL,           // optional, defaults to DefaultBaseURL
//	    Geocoder: ban,               // banx.Geocoder
//	})
//	data, err := src.Query(ctx, gazetteer.Listing{
//	    Address: "1 rue de Rivoli",
//	    Zip:     "75001",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	r := data.(*ademe.Result)
//	if r.IsEmpty() {
//	    fmt.Println("no DPE found in the ADEME catalog")
//	    return
//	}
//	if r.DPE != nil {
//	    fmt.Printf("DPE %s / GES %s (confidence: %s)\n",
//	        r.DPE.EtiquetteDPE, r.DPE.EtiquetteGES, r.Confidence)
//	}
//	if r.Logement != nil && r.Logement.AnneeConstruction != nil {
//	    fmt.Printf("built in %d\n", *r.Logement.AnneeConstruction)
//	}
//
// Empty responses (results: []) surface as IsEmpty()==true; the
// framework records StatusOKEmpty.
package ademe
