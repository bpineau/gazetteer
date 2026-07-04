// Package rnc is a gazetteer.Source that returns the copropriété CONTEXT
// recorded in the Registre National d'Immatriculation des Copropriétés
// (RNC, published by the ANAH on data.gouv.fr) for a building address.
//
// IMPORTANT — what this Source does NOT do. The RNC open-data export REDACTS
// both the financial declarations (impayés, charges, fonds de travaux) AND the
// legal-procedure/arrêté columns (administration provisoire art. 29-1,
// mandataire ad hoc art. 29-1A, plan de sauvegarde / carence, arrêtés de péril
// / insalubrité) — the ANAH notice documents them, but they are stripped from
// the published files. This Source therefore CANNOT emit a hard "copropriété
// en difficulté" verdict. It exposes the fields that ARE published, plus a
// deliberately low-confidence `Attention` triage hint — never a verdict.
//
// Triage value for a buyer (Result.Signals, stable keys):
//
//   - no_active_mandate — no mandate, or one expired with no successor
//     declared (a governance vacuum; a normal handover does not fire it).
//   - syndic_unknown / syndic_benevole — a non-professional or undeclared
//     syndic, but ONLY on a large copropriété (>= largeCoproLots lots), where
//     it is a red flag; on a small copro a bénévole syndic is normal.
//   - copro_aidee — an engaged ANAH subsidy (weak: since 2020 this includes
//     MaPrimeRénov' Copropriété, open to healthy copropriétés).
//   - fragile_profile — the "grand ensemble dégradé" archetype: large +
//     pre-1975 + inside a quartier prioritaire.
//
// Any present signal flags a lot as "worth checking the cahier des conditions
// de vente / the annuaire" before bidding; the hard distress check stays a
// manual step. A consumer (e.g. locador) can surface Attention per address the
// way it surfaces a rotten-zone flag.
//
// Cadastral parcelles: each Result carries the copropriété's cadastral parcel
// identifiers (Result.Cadastre, canonical 14-char refs). They are the reliable
// key for a building-level join — verifying or overriding the match below
// against DVF, the cadastre source, or an auction fiche's parsed parcelles.
//
// Granularity is the copropriété (a building / set of addresses), NOT the
// commune. A Listing is matched to a copro by GEO-PROXIMITY (the dataset
// geocodes each copro) plus NORMALIZED STREET name, scoped to the commune
// (INSEE); the Source is self-contained and needs no external geocoding. (The
// cadastral parcelles it now exposes let a caller tighten that match itself,
// but the Source does not consume a parcelle input.)
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
