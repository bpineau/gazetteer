// Package zonetendue provides a gazetteer Source that flags any French
// commune as "zone tendue" / "zone touristique et tendue" / non
// tendue, per the décret 2013-392 of 10 May 2013 as revised by décret
// 2025-1267 of 22 December 2025.
//
// The "zone tendue" classification carries direct legal consequences
// for residential leases:
//
//   - shortened tenant notice (préavis réduit à 1 mois) per loi ALUR
//   - taxe sur les logements vacants (TLV) eligibility (the 2013
//     décret was originally the TLV décret)
//   - majoration de la taxe d'habitation sur les résidences
//     secondaires (THRS) — communes may set up to +60 %
//   - encadrement loyer à la relocation (loyer plafond à la relocation
//     d'un bien)
//
// The "zone touristique et tendue" tier (added by the 2023 revision)
// covers communes where housing pressure is driven by tourist /
// short-stay markets — Paris, Côte d'Azur, mountain resorts, Île de
// Ré, etc.
//
// The Source is fully offline: a compact JSON map ships embedded
// under `data/`. Only communes that are flagged tendue (or were
// flagged TLV in the 2013 list) are stored explicitly — absence from
// the index means "non_tendue".
//
// Required Listing inputs:
//
//   - INSEE  (5-digit commune code). The Source emits
//     gazetteer.ErrInsufficientInputs when missing.
//
// Property type is irrelevant: the zonage classifies the whole
// commune.
//
// Example — wire the Source, query a Listing, and read the typed
// payload:
//
//	src := zonetendue.NewSource(zonetendue.Options{})
//	data, err := src.Query(ctx, gazetteer.Listing{INSEE: "75119"})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	r := data.(*zonetendue.Result)
//	fmt.Printf("tier=%s tendue=%v tlv2013=%v\n",
//	    r.Tier, r.IsTendue, r.FlaggedTLV2013)
//	if r.IsTendue {
//	    fmt.Println("tenant notice shortened to 1 month; TLV/THRS applicable")
//	}
package zonetendue

// Tier enumerates the three published classifications. Stable strings
// so downstream consumers can match without importing this package's
// constants.
type Tier string

const (
	// TierNonTendue covers communes outside the zonage. The Source
	// returns this on every miss in the embedded index — non-tendue is
	// the default (= absence from the dataset).
	TierNonTendue Tier = "non_tendue"

	// TierTendue covers communes flagged "1. Zone tendue" in the
	// post-2025-12-22 zonage.
	TierTendue Tier = "tendue"

	// TierTendueTouristique covers communes flagged "2. Zone
	// touristique et tendue".
	TierTendueTouristique Tier = "tendue_touristique"
)

// Confidence values returned in Result.Confidence.
const (
	// ConfidenceHigh : the answer comes from the legal reference
	// dataset itself.
	ConfidenceHigh = "high"
)
