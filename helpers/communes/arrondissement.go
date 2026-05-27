package communes

import "strings"

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
// through this helper before the lookup — otherwise every Paris /
// Lyon / Marseille listing returns an empty result.
//
// Returns insee unchanged for every other code (including INSEEs that
// are already parent commune for Paris/Lyon/Marseille).
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
