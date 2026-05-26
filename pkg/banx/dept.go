package banx

import "strings"

// DeptFromZip returns the canonical French département code derived from a
// 5-digit postal code. Returns "" when the input is not a 5-digit
// numeric zip.
//
// Encoding rules:
//
//   - Métropolitain (excluding Corsica): 2-digit prefix (e.g. 75001 → "75",
//     56000 → "56").
//   - Corsica: the 5-digit zip is split between Corse-du-Sud (2A) and
//     Haute-Corse (2B). Per La Poste, 20000–20199 belong to Corse-du-Sud
//     and 20200–20620 belong to Haute-Corse; the rule used here splits
//     on the third digit ("20[01]XX" → "2A", "20[2-7]XX" → "2B"), which
//     covers every urban auction commune (Ajaccio, Sartène, Bastia,
//     Biguglia).
//   - DOM-TOM (97xxx / 98xxx): returns the 3-digit prefix (971 Guadeloupe,
//     972 Martinique, 973 Guyane, 974 La Réunion, 976 Mayotte, 986/987/988
//     Wallis-Polynésie-Nouvelle-Calédonie).
//
// This is the canonical encoding that downstream tables key on
// (tribunals.department_code, PappersDeptToSlug). For the looser
// "do these two zips fall in the same département for cross-source
// matching?" predicate, use ZipsShareDepartment — which treats every
// Corsican zip as belonging to a single département so cross-island
// matching still folds.
func DeptFromZip(zip string) string {
	zip = strings.TrimSpace(zip)
	if len(zip) != 5 {
		return ""
	}
	for i := range 5 {
		c := zip[i]
		if c < '0' || c > '9' {
			return ""
		}
	}
	prefix := zip[:2]
	switch prefix {
	case "20":
		// 2A vs 2B split — see godoc above.
		if zip[2] == '0' || zip[2] == '1' {
			return "2A"
		}
		return "2B"
	case "97", "98":
		// DOM-TOM: 3-digit dept.
		return zip[:3]
	default:
		return prefix
	}
}

// deptMatchKey returns the prefix used to test cross-zip département
// membership. Unlike DeptFromZip it does NOT split Corsica (both 2A and
// 2B zips share the "20" key) so that two zips on the same island fold
// together for cross-source enricher matching. It also tolerates
// shorter-than-5-char inputs by falling back to the input itself
// (so equality remains the safe default for malformed values).
func deptMatchKey(zip string) string {
	if len(zip) < 2 {
		return zip
	}
	if zip[0] == '9' && (zip[1] == '7' || zip[1] == '8') && len(zip) >= 3 {
		return zip[:3]
	}
	return zip[:2]
}
