// Package rpls is a gazetteer.Source that returns the per-commune share
// of social housing (logement locatif social), expressed as a percentage
// of résidences principales, computed under the loi SRU article 55
// framework.
//
// The signal matters for a rental investor because the SRU rate is a
// strong proxy for the social mix of a commune's housing stock:
//
//   - Very low (<3 %) communes are rural or upscale residential — most
//     never reached the SRU obligation threshold.
//   - The 3–15 % band is the bulk of urban France: balanced mix.
//   - 15–30 % communes carry a strong public-housing presence; tenant
//     turnover and rent ceilings differ from the private-market norm.
//   - 30 %+ is the territory where mono-locatif HLM dynamics dominate
//     (Sevran, La Courneuve, Aulnay…) — strong distress signal when
//     compounded with high delinquance and low IPS.
//
// Granularity is the commune (5-digit INSEE). The dataset does NOT
// publish per-arrondissement rows for Paris / Lyon / Marseille; the
// Source folds them onto the parent commune INSEE via the
// `helpers/communes` arrondissement helper.
//
// The Source is fully offline: the merged crosswalk ships embedded
// under `data/rpls_communes.json.gz`.
//
// Required Listing inputs:
//
//   - INSEE (5-digit commune code). The Source emits
//     gazetteer.ErrInsufficientInputs when missing.
//
// Property type is irrelevant — the SRU rate is a commune-wide attribute.
//
// Upstream data: data.gouv.fr "Taux de logements sociaux dans les
// Communes" (dataset ressource r/b0d30277-3a14-4673-a988-2fa6c11e030c,
// relayed from Caisse des Dépôts open-data). Licence Ouverte 2.0.
// Vintage: 2024 (frozen 2025-01-01). Re-run the build script
// (`/tmp/gazetteer-data/build_rpls.py`) against the latest CSV to
// refresh the embedded blob.
//
// Tier thresholds — calibrated against the 2024 distribution:
//
//   - rural    : rate <  3 %  (≈ 63 % of communes)
//   - mixte    : rate ∈ [3, 15)   (≈ 28 %)
//   - fort     : rate ∈ [15, 30)  (≈  6 %)
//   - satured  : rate ≥ 30 %      (≈  2 %, ~600 communes)
//
// Risk-flag mapping for investor consumers:
//
//   - rural / mixte → healthy
//   - fort          → mixed, watch QPV + revenu + delinquance
//   - satured       → strong distress signal; compose with delinquance + filosofi
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := rpls.NewSource(rpls.Options{})
//	data, err := src.Query(ctx, gazetteer.Listing{INSEE: "93071"}) // Sevran
//	if err != nil { log.Fatal(err) }
//	r := data.(*rpls.Result)
//	if r.IsEmpty() {
//	    fmt.Println("commune absent from the SRU dataset")
//	    return
//	}
//	fmt.Printf("LLS rate: %.1f %% (%s)\n", r.LLSRate, r.Tier)
package rpls

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers can match on them without importing this
// package's constants.
const (
	ConfidenceHigh = "high"
	ConfidenceNone = ""
)

// Tier is a coarse, distribution-relative bucket on the SRU rate.
// Informative only — never folded into a score by this Source.
type Tier string

const (
	TierUnknown Tier = "unknown"
	TierRural   Tier = "rural"
	TierMixte   Tier = "mixte"
	TierFort    Tier = "fort"
	TierSatured Tier = "satured"
)
