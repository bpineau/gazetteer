// Package gazetteer is a generic Go library that compiles geographic and
// real-estate data about French addresses from multiple sources.
//
// # Concepts
//
//   - Listing — the universal input (address + coords + property attrs)
//   - Source — a named, versioned data origin (Query(ctx, listing) → (any, error))
//   - Result — the framework envelope around a Source's typed payload
//   - Dossier — the aggregated output of one Client.Collect call
//   - Builder / Client — configure sources, then run them in parallel
//   - Normalizer — canonicalises a free-text address into a Listing
//   - kvcache.Cache — pluggable persistent KV cache consumed by Sources
//     that need cross-run memo (e.g. dvf section catalogue,
//     banx.CachedGeocoder)
//
// # Quick start
//
//	dvfSrc, err := dvf.NewSource(dvf.Options{HTTP: hc, Geocoder: ban})
//	if err != nil { /* handle */ }
//	osmSrc := osm.NewSource(osm.Options{}) // call UpdateCatalog later
//	client, _ := gazetteer.NewBuilder().With(dvfSrc).With(osmSrc).Build()
//
//	d := client.Collect(ctx, listing)
//	if r, ok := gazetteer.Get[*dvf.Result](d, dvf.Name); ok && r.ValueEURPerM2Cents != nil {
//	    fmt.Printf("%.0f €/m²\n", float64(*r.ValueEURPerM2Cents)/100)
//	}
//
// # Status interpretation
//
// Each Source's Result carries a Status. OK / OKEmpty are successful;
// SkippedPrereq means the Source declined (inputs missing / unsupported
// property type); FailedTransient / AntiBot / Outdated / Permanent are
// failure modes with distinct retry semantics.
//
// # Plugins
//
// Out-of-tree Source packages implement the same Source interface and
// register their typed payload via gazetteer.Register in init().
// Callers wire them with builder.With(...) like any official source.
//
// See doc/ in the repository root for long-form documentation:
// concepts, sources, plugins, circuit_breakers, caching, testing, cli.
package gazetteer
