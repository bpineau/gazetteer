// Package zonageabc provides a gazetteer Source that classifies any
// French commune into the official A bis / A / B1 / B2 / C zonage.
//
// The zonage is the canonical tension classification published by the
// Ministère du Logement and used by every national housing policy that
// gates on local supply / demand imbalance (Pinel, PTZ, Loc'Avantages,
// Action Logement, plafonds APL, etc.). A and A bis are the tightest
// markets ; C the most balanced.
//
// The Source is fully offline: the per-commune classification ships
// embedded under `data/` as a compact JSON map.
//
// Required Listing inputs:
//
//   - INSEE  (5-digit commune code). The Source emits
//     gazetteer.ErrInsufficientInputs when missing — callers are
//     responsible for resolving INSEE from (zip, city) via a geocoder
//     before invoking this Source.
//
// Property type is irrelevant: the zonage applies to the commune as a
// whole.
package zonageabc

// Confidence values returned in Result.Confidence. Stable strings so
// downstream consumers can match on them without importing this
// package's constants. The Source returns ConfidenceHigh on every
// match (the dataset is the legal reference itself) and ConfidenceNone
// for communes missing from the dataset (rare; only fires for INSEE
// codes that do not exist in the September 2025 revision — typically
// historic communes that have since been fused or split).
const (
	ConfidenceHigh = "high"
	ConfidenceNone = ""
)

// Zone enumerates the five published zonage tiers. Stable strings so
// downstream consumers can match on them without importing this
// package's constants.
type Zone string

const (
	ZoneUnknown Zone = ""
	ZoneAbis    Zone = "Abis"
	ZoneA       Zone = "A"
	ZoneB1      Zone = "B1"
	ZoneB2      Zone = "B2"
	ZoneC       Zone = "C"
)

// TensionScore returns a coarse 0..4 tension score for the zone. Higher
// = tighter market. Useful for downstream scoring layers that want a
// single ordinal scalar rather than the categorical label.
//
//	ZoneAbis -> 4 (Paris + petite couronne IDF + a few hot spots)
//	ZoneA    -> 3 (grande couronne IDF, Côte d'Azur, Genevois français)
//	ZoneB1   -> 2 (large metro areas: Lyon, Bordeaux, Nantes, etc.)
//	ZoneB2   -> 1 (medium cities, growing periurban communes)
//	ZoneC    -> 0 (balanced / loose markets)
//	unknown  -> -1
func TensionScore(z Zone) int {
	switch z {
	case ZoneAbis:
		return 4
	case ZoneA:
		return 3
	case ZoneB1:
		return 2
	case ZoneB2:
		return 1
	case ZoneC:
		return 0
	default:
		return -1
	}
}
