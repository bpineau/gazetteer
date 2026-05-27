// Package cadastre is a gazetteer.Source for the French cadastre.
//
// Given a Listing with usable lat/lon, the Source returns the
// cadastral parcel containing that point — parcel id, contenance
// (surface), commune INSEE, and a clickable link to the Etalab
// cadastre viewer (`https://cadastre.data.gouv.fr/map?...`). When
// IncludeBati is opted-in, the Source also computes the count and
// total footprint area of buildings sitting on the parcel by filtering
// the commune-wide PCI bâtiments dump down to the polygons whose
// centroid falls inside the parcel.
//
// # Strategy
//
// The Source queries two upstreams:
//
//   - `apicarto.ign.fr/api/cadastre/parcelle?geom={Point}` — IGN's
//     public API Carto returns a GeoJSON FeatureCollection of the
//     parcel(s) intersecting the queried lat/lon. No auth, no quota
//     documented.
//   - (opt-in) `cadastre.data.gouv.fr/bundler/cadastre-etalab/communes/
//     {insee}/geojson/batiments` — Etalab's per-commune building
//     polygon dump (Content-Type: application/vnd.geo+json, served
//     gzipped, a few MB per commune). Cached in-process per INSEE so
//     a single commune is fetched at most once per run.
//
// # Error semantics
//
//   - Missing lat/lon                                   → ErrInsufficientInputs
//   - URL builder rejects coords (NaN, out-of-range)    → ErrInsufficientInputs
//   - API Carto HTTP 5xx / transport / parse failure    → ErrUpstreamUnavailable
//   - API Carto HTTP 4xx other than 404                 → ErrUpstreamPermanent
//   - API Carto HTTP 404 / empty FeatureCollection      → IsEmpty Result
//     (Status == StatusOKEmpty)
//   - Bâti enrichment failure (HTTP/parse, opt-in only) → soft-fail:
//     parcel data still returned, Evidence.BatiError stamped, bâti
//     fields nil.
//
// # Rhythm & rate-limit
//
// Standard rhythm. ~1 req/s per upstream (per-host). API Carto is
// fast (~150 ms) ; the bâtiments dump dominates wall time when bâti
// is enabled — that's why the in-process cache exists.
//
// # Example
//
//	src := cadastre.NewSource(cadastre.Options{IncludeBati: true})
//	lat, lon := 48.8566, 2.3522
//	data, err := src.Query(ctx, gazetteer.Listing{Lat: &lat, Lon: &lon})
//	if err != nil { log.Fatal(err) }
//	r := data.(*cadastre.Result)
//	if r.IsEmpty() {
//	    fmt.Println("no parcel under this point")
//	    return
//	}
//	p := r.Parcels[0]
//	fmt.Printf("parcel %s — %d m² — %s\n", p.ID, p.ContenanceM2, p.MapURL)
//	if r.BatiM2 != nil && r.EmpriseRatio != nil {
//	    fmt.Printf("emprise: %.0f m² (%.0f %%)\n", *r.BatiM2, *r.EmpriseRatio*100)
//	}
package cadastre
