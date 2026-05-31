// Package qpv is a gazetteer.Source that answers whether a listing sits
// inside a Quartier Prioritaire de la Politique de la Ville (QPV) — by
// point-in-polygon when the listing has coordinates, falling back to
// commune-level membership otherwise.
//
// QPV is the official zoning policy administered by the ANCT (decree
// 2023-1314, effective 1 January 2024) that designates the most
// disadvantaged urban neighbourhoods. The label gates several fiscal
// and social devices: rental investors targeting QPV-located stock
// face specific guardrails (Pinel restrictions, exonérations TFPB,
// ZFU exemptions) and a different tenant demographic.
//
// Resolution:
//
//   - With Lat/Lon: bbox-prefiltered point-in-polygon over the embedded
//     QPV 2024 contours. A point inside a QPV returns that single QPV
//     (MatchLevel "point", high confidence); a point outside every QPV
//     returns HasQPV=false (high confidence) plus, when one is within
//     NearestQPVMaxMeters, a NearestQPV hint that is deliberately kept
//     OUT of HasQPV. This is the address-level answer.
//   - Without Lat/Lon: commune-level fallback (arrondissements folded to
//     the parent commune), returning every QPV in the commune at
//     MatchLevel "commune", medium confidence — the coarse "does this
//     town host QPVs?" signal.
//
// The Source is fully offline: the QPV 2024 contour polygons (ANCT,
// data.gouv.fr, WGS84 GeoJSON, métropole + outre-mer) ship embedded and
// gzipped under `data/`.
//
// Required Listing inputs:
//
//   - INSEE (5-digit commune code). The Source emits
//     gazetteer.ErrInsufficientInputs when missing.
//   - Lat/Lon (optional but strongly recommended) — unlocks the
//     point-in-polygon path; without them the answer is commune-level only.
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := qpv.NewSource(qpv.Options{})
//	data, err := src.Query(ctx, gazetteer.Listing{INSEE: "75118", Lat: &lat, Lon: &lon})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	r := data.(*qpv.Result)
//	if r.IsEmpty() {
//	    fmt.Println("not in a QPV")
//	    return
//	}
//	for _, q := range r.QPVs {
//	    fmt.Printf("  %s — %s\n", q.Code, q.Label)
//	}
package qpv
