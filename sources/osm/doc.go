// Package osm is a gazetteer.Source that computes the walking
// distance from a listing to the nearest métro / RER / tram /
// Transilien / train station, against an offline OpenStreetMap
// catalog refreshed out-of-band.
//
// The Source requires (Listing.Lat, Listing.Lon) and a non-empty
// catalog. Catalogs are produced by a CatalogFetcher (Overpass API)
// and installed via UpdateCatalog. An empty catalog is rejected at
// install time so a failed background refresh cannot silently
// discard a loaded one. Query is concurrency-safe with respect to
// in-flight UpdateCatalog calls — the catalog pointer is swapped
// atomically.
//
// The Source returns a *Result with the nearest station name, type,
// lines, walk distance in metres (haversine × WalkSinuosityMultiplier)
// and walk minutes. When the nearest station is beyond
// MaxNearestStationMeters the Source returns IsEmpty()==true with
// SkipReason = SkipReasonOutOfRange.
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := osm.NewSource(osm.Options{})
//	src.UpdateCatalog(loadedCatalog)
//	data, err := src.Query(ctx, gazetteer.Listing{
//	    Lat: &lat, Lon: &lon,
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	r := data.(*osm.Result)
//	if r.IsEmpty() {
//	    fmt.Println("no transit station within range")
//	    return
//	}
//	fmt.Printf("nearest %s station: %s (%d min walk, lines %v)\n",
//	    r.NearestTransitType, r.NearestTransitName,
//	    r.NearestTransitWalkMin, r.NearestTransitLines)
//
// To refresh the catalog out-of-band:
//
//	fetcher := osm.NewCatalogFetcher(httpClient, slog.Default())
//	cat, err := fetcher.Fetch(ctx, "")
//	src.UpdateCatalog(cat)
package osm
