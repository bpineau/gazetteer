// Package catnat is a gazetteer.Source for the per-commune history of
// recognised natural disasters ("arrêtés de catastrophe naturelle", GASPAR).
//
// Where georisques reports the hazard (the modelled exposure — flood zones,
// clay soil, …), catnat reports the realised sinistralité: how many CatNat
// decrees the commune has actually accumulated since 1982, by category, and how
// recently. That history is what a PNO insurer prices on, and it reveals risks
// the zoning misses — notably the drought / clay-shrinkage decrees that drive
// foundation cracking across the Paris ring.
//
// Given a Listing's commune the Source returns the total decree count, the
// recent-window count, the per-category breakdown, the latest event year and a
// frequency tier. The typed Result implements appraisal.HazardReporter, so the
// confirmed natural-risk categories flow into appraisal.HazardProfile alongside
// georisques.
//
// The Source is fully offline: a gzipped per-commune aggregate ships under
// `data/`, refreshable from the Géorisques GASPAR export.
package catnat
