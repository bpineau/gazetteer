// Package osm is a gazetteer.Source that estimates the walking distance
// from a listing to the nearest métro / RER / tram / Transilien / train
// station, against an OpenStreetMap station catalog.
//
// ESTIMATES: the distance is the great-circle one scaled by
// WalkSinuosityMultiplier, not a route over a street graph. Nothing
// between the address and the station is consulted, so a river, a
// railway cutting or a walled estate is invisible and the nearest
// station as the crow flies need not be the nearest on foot. It is an
// order of magnitude for ranking addresses, not an itinerary.
//
// The catalog is resolved like every other block dataset: an embedded
// baseline (metropolitan France, ~9k stations) is used unless a refreshed
// copy is present in the datadir (see the dataset package). NewSource loads
// it automatically — zero-value Options work out of the box. The Source
// requires (Listing.Lat, Listing.Lon).
//
// Live fallback: when the catalog has no station within
// MaxNearestStationMeters of a query point — a zone the baseline does not
// cover (DOM-TOM, a brand-new station), or no catalog at all — and an
// Options.Fetcher is configured, the Source queries Overpass live around
// that point. Catalog-first keeps the common case offline and fast; the live
// path covers the gaps.
//
// Refresh: `gazetteer refresh osm_transit` rebuilds the catalog from a live
// Overpass refresh (one query per department) into the datadir; it is
// idempotent (a present, current catalog is skipped). There is no automatic
// periodic refresh — métro/RER stations change a couple of times a year, so
// the embedded baseline is refreshed on demand.
//
// The Source returns a *Result with the nearest station name, type, lines,
// walk distance in metres (haversine × WalkSinuosityMultiplier) and walk
// minutes. When the nearest station is beyond MaxNearestStationMeters (and
// the live fallback finds nothing closer) the Source returns IsEmpty()==true
// with SkipReason = SkipReasonOutOfRange.
//
// Example — wire the Source (with the live fallback) and query a Listing:
//
//	src := osm.NewSource(osm.Options{
//	    DataDir: dataDir, // refreshed catalog override; embedded baseline otherwise
//	    Fetcher: osm.NewHTTPOverpassFetcher(httpClient, ""), // optional live fallback
//	})
//	data, err := src.Query(ctx, gazetteer.Listing{Lat: &lat, Lon: &lon})
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
package osm
