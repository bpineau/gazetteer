// Package iris resolves a coordinate to its INSEE IRIS — the sub-commune
// statistical zone (≈ 2 000 inhabitants) that is the finest official mesh for
// census, income and many other datasets.
//
// It serves two roles from one embedded dataset:
//
//   - as a gazetteer.Source, it returns the IRIS code, name and type for a
//     Listing's coordinates (a precise sub-commune locator in the Dossier);
//   - as a gazetteer.IRISResolver (ResolveIRIS), it lets a BANNormalizer
//     populate Listing.IRIS, so IRIS-keyed sources resolve at full granularity.
//
// Resolution is point-in-polygon over the embedded IRIS contours (helpers/geopoly,
// with a bounding-box pre-filter). Scope (v1): Île-de-France (~5 300 IRIS),
// which keeps the gzipped contours embeddable; a point outside it yields
// StatusOKEmpty / ResolveIRIS ok=false.
//
// The Source is fully offline: a gzipped compact contour snapshot ships under
// `data/`, refreshable from the region's Opendatasoft portal.
package iris
