// Package dvf is a gazetteer.Source for the Demandes de Valeurs
// Foncières (DVF) Etalab API published via data.gouv.fr — historical
// real-estate transaction prices.
//
// The Source resolves the listing's INSEE via the BAN cascade and
// walks a 4-tier fallback ladder via helpers/fallback.Walk:
//
//  1. address_radius — 500 m disk around (Listing.Lat, Listing.Lon),
//     MinSample 12.
//  2. commune — the listing's INSEE.
//  3. neighborhood — commune + its haversine neighbours.
//  4. department — entire département.
//
// The winning tier is recorded in Evidence.LevelUsed. The Source
// returns median, p25 and p75 EUR/m² (in cents) and satisfies
// appraisal.PriceEstimator.
//
// Caching: the per-INSEE cadastral section catalog is memoised via a
// kvcache.Cache (Options.SectionCache). Use an in-memory backend for
// tests and a persistent backend for long-running batches.
//
// Circuit breaker: 3 consecutive transport errors OR 3 consecutive
// HTTP 429s trip the breaker. Query then returns ErrCircuitTripped
// (which matches gazetteer.ErrSourceCircuitTripped for cross-source
// matching).
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := dvf.NewSource(dvf.Options{
//	    HTTP:     hc,                    // *httpx.Client
//	    Geocoder: ban,                   // banx.Geocoder
//	    Communes: communes.MustDefault(),// optional, embedded default
//	})
//	data, err := src.Query(ctx, gazetteer.Listing{
//	    Address: "10 rue de Rivoli", Zip: "75001",
//	    PropertyType: gazetteer.PropertyApartment,
//	    SurfaceM2: &surface,
//	})
//	if err != nil { log.Fatal(err) }
//	r := data.(*dvf.Result)
//	if r.IsEmpty() {
//	    fmt.Println("no DVF transactions matched")
//	    return
//	}
//	if r.ValueEURPerM2Cents != nil {
//	    eurPerM2 := float64(*r.ValueEURPerM2Cents) / 100
//	    fmt.Printf("median %.0f €/m² over %d sales (tier=%s, confidence=%s)\n",
//	        eurPerM2, r.SampleSize, r.Evidence.LevelUsed, r.Confidence)
//	}
package dvf
