// Package bdnb is a gazetteer.Source for the Base de Données Nationale
// des Bâtiments (BDNB) `batiment_groupe_complet` PostgREST endpoint.
//
// The Source resolves the listing's 5-digit INSEE via the BAN cascade
// (banx.INSEEResolver), queries BDNB with an ILIKE address pattern
// over BDNB rows for that INSEE, picks the most likely candidate and
// returns a *Result carrying building age, construction class,
// dwelling count and parcel surface.
//
// Quota: BDNB enforces a rolling 10 000-request budget per 30-day
// window (no API key required), surfaced via the `x-quota-remaining`
// response header and HTTP 429 once the budget is gone. Wire a
// helpers/circuit.HTTPFetcher to trip the breaker on either signal.
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := bdnb.NewSource(bdnb.Options{
//	    BaseURL:  srv.URL,    // optional, defaults to package var BaseURL
//	    Geocoder: ban,        // banx.Geocoder (forward + reverse cascade)
//	})
//	data, err := src.Query(ctx, gazetteer.Listing{
//	    Address: "10 rue de Rivoli", Zip: "75001",
//	})
//	if err != nil { log.Fatal(err) }
//	r := data.(*bdnb.Result)
//	if r.IsEmpty() {
//	    fmt.Println("no BDNB row found for this address")
//	    return
//	}
//	if r.Building != nil && r.Building.AnneeConstruction != nil {
//	    fmt.Printf("built in %d, %d dwellings\n",
//	        *r.Building.AnneeConstruction, intOrZero(r.Building.NbLog))
//	}
//	if r.DPE != nil && r.DPE.ClasseBilan != "" {
//	    fmt.Printf("DPE bilan: %s\n", r.DPE.ClasseBilan)
//	}
package bdnb
