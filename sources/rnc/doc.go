// Package rnc is a gazetteer.Source that returns the copropriété CONTEXT
// recorded in the Registre National d'Immatriculation des Copropriétés
// (RNC, published by the ANAH on data.gouv.fr) for a building address.
//
// IMPORTANT — what this Source does NOT do. The public RNC open-data file
// REDACTS both the financial declarations (impayés, charges, fonds de
// travaux) AND the legal-procedure columns (administration provisoire
// art. 29-1, mandataire ad hoc art. 29-1A, plan de sauvegarde / carence,
// arrêtés de péril / insalubrité). This Source therefore CANNOT emit a hard
// "copropriété en difficulté" verdict. It exposes the context fields that
// ARE published, plus a deliberately low-confidence `Attention` triage hint
// (see Result.Signals) — never a verdict.
//
// Signal value for a buyer: the context (lots, construction period, syndic
// type, QPV) plus the weak governance hints (no active syndic mandate,
// unknown/amateur syndic, ANAH-subsidized) flag a lot as "worth checking the
// cahier des conditions de vente / consulting the registre" before bidding.
// The hard distress check stays a manual step.
//
// Granularity is the copropriété (a building / set of addresses), NOT the
// commune. A Listing is matched to a copro by GEO-PROXIMITY (the dataset
// geocodes each copro) plus NORMALIZED STREET name, scoped to the commune
// (INSEE). No cadastral parcelle is used: the dataset's own lat/long is the
// matching key, so the Source is self-contained and needs no external
// geocoding or parcelle resolution.
//
// The Source is fully offline: the processed dataset ships embedded under
// `data/rnc_coproprietes.json.gz`.
//
// Required Listing inputs:
//
//   - INSEE (5-digit commune code). Without it the Source emits
//     gazetteer.ErrInsufficientInputs.
//   - Lat/Lon (strongly recommended — the primary matching key). Without
//     coordinates the Source falls back to a single-candidate street match
//     within the commune, which often yields no match.
//   - Address (used to normalize the street for the match + confidence).
//
// Upstream data: Registre National d'Immatriculation des Copropriétés —
// data.gouv.fr dataset "registre-national-dimmatriculation-des-coproprietes",
// resource "RNIC - Actualisation quotidienne" (the daily "with-qpv" CSV,
// ~400 MB). Refresh with `gazetteer refresh --go-embed-update rnc`.
//
// Example:
//
//	src := rnc.NewSource(rnc.Options{})
//	data, err := src.Query(ctx, gazetteer.Listing{INSEE: "93066", Lat: &lat, Lon: &lon, Address: "6 allée Ambroise Thomas"})
//	if err != nil {
//		log.Fatal(err)
//	}
//	r := data.(*rnc.Result)
//	if !r.IsEmpty() && r.Attention {
//		fmt.Printf("copro à vérifier (%v, conf=%s)\n", r.Signals, r.Confidence)
//	}
package rnc
