// Package cdsr is a gazetteer.Source for the Île-de-France region's
// "Copropriétés en Difficulté Soutenues par la Région" (CDSR) — condominiums
// labelled severely distressed and granted a regional subsidy for thermal
// renovation. It is a small, curated, high-precision red-flag: when a CDSR
// copro sits near a prospective purchase, the surrounding condominium stock
// carries documented structural difficulty.
//
// The Source is purely spatial: given the listing's coordinates it reports the
// nearest labelled copro (within MaxNearestMeters) and how many fall within
// 500 m and 3 km, with each match's name, address, lot count and label year.
// There is no fuzzy name matching — both the listing and the catalog are
// geolocated, so the haversine distance is the whole contract.
//
// Coverage is Île-de-France only and intentionally sparse (~17 of the most
// severe, region-intervened cases). A "no CDSR within 3 km" answer is the
// common, reassuring case and surfaces as StatusOKEmpty.
//
// The Source is fully offline: an embedded JSON snapshot ships under `data/`
// and is refreshable from the region's Opendatasoft portal.
package cdsr
