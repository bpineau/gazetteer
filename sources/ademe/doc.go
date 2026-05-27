// Package ademe is a gazetteer.Source for the ADEME `dpe03existant`
// dataset (energy-performance certificates) published via
// data.gouv.fr's data-fair API.
//
// The Source resolves the listing's zip via a banx.Geocoder when
// missing, hits the ADEME endpoint with an address pattern derived
// from fraddr.Parse, then picks the best candidate by street-number
// match first and — when several rows share the same number and the
// Listing carries a SurfaceM2 anchor — the row whose
// surface_habitable_logement is closest to that anchor. Without a
// SurfaceM2 anchor the picker preserves the upstream
// (_score, date_etablissement_dpe desc) order. Returns a *Result
// carrying DPE label, GES label, surface, build year and dwelling
// type.
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
