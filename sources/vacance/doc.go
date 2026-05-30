// Package vacance is a gazetteer.Source that returns the
// per-commune DEMOGRAPHIC vacancy rate from INSEE's Recensement de la
// Population 2021 ("base communale logement").
//
// IMPORTANT — naming disambiguation: the existing sources/vacance
// source surfaces the FISCAL vacancy status (TLV-2013 zone tendue
// classification used by Bercy to assess the Taxe sur les Logements
// Vacants). This source is the DEMOGRAPHIC rate — the share of LOGVAC
// over LOG observed in the census, regardless of any taxation gate.
// The two signals are correlated but distinct: a commune outside the
// TLV-2013 zone can still have a high vacancy rate (déprise) and
// vice-versa.
//
// The signal matters for a rental investor because the vacancy rate
// is a leading proxy for letting risk and time-on-market:
//
//   - Very low (<4 %) — tension forte, frictional vacancy only; rents
//     and time-to-let move in the landlord's favour.
//   - 4–8 % — normal frictional band; balanced market.
//   - 8–15 % — élevé; risk of voids, slow re-letting.
//   - 15 %+ — déprise; "personne ne veut louer" territory; structural
//     surplus of stock. Combine with the demographic trend before
//     buying anything.
//
// Granularity is the commune (5-digit INSEE), INCLUDING the per-
// arrondissement rows for Paris (75101..75120), Lyon (69381..69389) and
// Marseille (13201..13216). The Source does NOT fold arrondissements —
// the INSEE census publishes one row per arrondissement so callers can
// see, e.g., Paris 1er vacancy ≠ Paris 18e vacancy.
//
// The Source is fully offline: the merged dataset ships embedded
// under `data/vacance_communes.json.gz`.
//
// Required Listing inputs:
//
//   - INSEE (5-digit commune code). The Source emits
//     gazetteer.ErrInsufficientInputs when missing.
//
// Property type is irrelevant — the vacancy rate is a commune-wide
// attribute.
//
// Upstream data: INSEE Recensement de la Population 2021 — "base
// communale logement" (file 8202349 on insee.fr). Standard INSEE open
// data terms. Vintage: census 2021, published December 2025. Re-run
// the build script (`/tmp/gazetteer-data/build_vacance.py`)
// against a fresh CSV to refresh the embedded blob.
//
// Tier thresholds — calibrated against the 2021 distribution:
//
//   - tendu    : vacancy <  4 %  (≈ 14 % of communes)
//   - normal   : vacancy ∈ [4, 8)    (≈ 38 %)
//   - élevé    : vacancy ∈ [8, 15)   (≈ 39 %)
//   - déprise  : vacancy ≥ 15 %      (≈ 10 %)
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := vacance.NewSource(vacance.Options{})
//	data, err := src.Query(ctx, gazetteer.Listing{INSEE: "42218"}) // Saint-Étienne
//	if err != nil { log.Fatal(err) }
//	r := data.(*vacance.Result)
//	if r.IsEmpty() {
//	    fmt.Println("commune absent from the census base logement")
//	    return
//	}
//	fmt.Printf("vacancy rate: %.1f %% (%s)\n", r.VacancyRate, r.Tier)
package vacance

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers can match on them without importing this
// package's constants.
const (
	ConfidenceHigh = "high"
	ConfidenceNone = ""
)

// Tier is a coarse, distribution-relative bucket on the vacancy rate.
// Informative only — never folded into a score by this Source.
type Tier string

const (
	TierUnknown Tier = "unknown"
	TierTendu   Tier = "tendu"
	TierNormal  Tier = "normal"
	TierEleve   Tier = "élevé"
	TierDeprise Tier = "déprise"
)
