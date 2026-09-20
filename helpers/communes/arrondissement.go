package communes

import (
	"fmt"
	"strings"
)

// ArrondissementParents returns the explicit arrondissement→parent INSEE
// mapping (Paris 75101..75120 → 75056, Lyon 69381..69389 → 69123,
// Marseille 13201..13216 → 13055) as a fresh map. It is the enumerable
// complement of FoldArrondissement, for callers that need the alias codes
// themselves (e.g. a dataset transform expanding parent-keyed rows to
// arrondissement keys).
func ArrondissementParents() map[string]string {
	out := make(map[string]string, 45)
	add := func(prefix string, lo, hi int, parent string) {
		for i := lo; i <= hi; i++ {
			out[fmt.Sprintf("%s%02d", prefix, i)] = parent
		}
	}
	add("751", 1, 20, "75056")  // Paris
	add("693", 81, 89, "69123") // Lyon
	add("132", 1, 16, "13055")  // Marseille
	return out
}

// IsArrondissementParent reports whether insee is one of the three parent
// commune codes Paris (75056), Lyon (69123) and Marseille (13055) carry
// alongside their arrondissement codes.
//
// It is the test a dataset keyed by ARRONDISSEMENT needs, and the mirror of
// FoldArrondissement, which a dataset keyed by parent needs. Getting the
// direction wrong is silent either way: the lookup simply misses and the
// caller reads "no data for this commune" about Paris. ResolveINSEE("Paris",
// "75000") returns the parent code, so an ordinary input reaches it.
func IsArrondissementParent(insee string) bool {
	switch insee {
	case "75056", "69123", "13055":
		return true
	default:
		return false
	}
}

// FoldArrondissement maps Paris / Lyon / Marseille arrondissement
// INSEE codes onto their parent commune INSEE. Datasets published by
// the French administration usually carry one row per parent commune
// only (75056 for Paris, 69123 for Lyon, 13055 for Marseille). The
// arrondissement-level codes (75101..75120, 69381..69389,
// 13201..13216) inherit the same value.
//
// The BAN forward-geocoder returns the arrondissement-level INSEE for
// any Paris / Lyon / Marseille address, so a Source that looks up an
// embedded dataset keyed by parent commune MUST fold the input INSEE
// through this helper before the lookup: otherwise every Paris /
// Lyon / Marseille listing returns an empty result.
//
// Returns insee unchanged for every other code (including the parent
// commune codes of Paris / Lyon / Marseille themselves).
func FoldArrondissement(insee string) string {
	if len(insee) != 5 {
		return insee
	}
	switch {
	case strings.HasPrefix(insee, "751"): // Paris 75101..75120 -> 75056
		return "75056"
	case strings.HasPrefix(insee, "6938"): // Lyon 69381..69389 -> 69123
		return "69123"
	case strings.HasPrefix(insee, "132"): // Marseille 13201..13216 -> 13055
		return "13055"
	default:
		return insee
	}
}
