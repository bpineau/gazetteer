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
//   - Cache — pluggable backend for intermediate state (MemCache default)
//   - Normalizer — canonicalises a free-text address into a Listing
//
// # Quick start
//
//	dvfSrc, err := dvf.NewSource(dvf.Options{HTTP: hc, Geocoder: ban})
//	if err != nil { /* handle */ }
//	osmSrc := osm.NewSource(osm.Options{}) // call UpdateCatalog later
//	client, _ := gazetteer.NewBuilder().With(dvfSrc).With(osmSrc).Build()
//
//	d := client.Collect(ctx, listing)
//	if r, ok := gazetteer.Get[*dvf.Result](d, dvf.Name); ok {
//	    fmt.Println(r.MedianEurPerM2)
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
// Out-of-tree Source packages (e.g. private antibot scrapers) implement
// the same Source interface and register their typed payload via
// gazetteer.Register in init(). Callers wire them with builder.With(...)
// like any official source.
//
// See doc/gazetteer/2026-05-25-extraction-design.md for the full design.
package gazetteer
