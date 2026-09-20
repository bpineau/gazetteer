package banx

import "github.com/bpineau/gazetteer/helpers/communes"

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
	// Delegates to the canonical implementation in helpers/communes —
	// administrative-geography knowledge has one home; this alias stays
	// for banx's existing callers.
	return communes.DeptFromZip(zip)
}

// saintMartinBarthZips maps the two postal codes that a 3-digit prefix cannot
// separate from Guadeloupe onto their real département. Saint-Barthélemy (977)
// and Saint-Martin (978) were split off Guadeloupe in 2007 but kept postal
// codes inside the 971xx range, so "971" is the prefix of three départements,
// not one — and the two islands sit ~230 km from Guadeloupe.
var saintMartinBarthZips = map[string]string{
	"97133": "977", // Saint-Barthélemy
	"97150": "978", // Saint-Martin
}

// deptMatchKey returns the key used to test cross-zip département membership.
// Unlike DeptFromZip it does NOT split Corsica (both 2A and 2B zips share the
// "20" key) so that two zips on the same island fold together for
// cross-source enricher matching.
//
// Anything that is not a well-formed 5-digit zip returns the input unchanged,
// so the comparison degrades to equality. That matters for the zip whose
// leading zero was eaten by a spreadsheet or a JSON number: "1000" is
// Bourg-en-Bresse in the Ain (01), and a 2-character prefix would read "10",
// the Aube, and declare it the same département as Troyes.
func deptMatchKey(zip string) string {
	if !isFiveDigitZip(zip) {
		return zip
	}
	if d, ok := saintMartinBarthZips[zip]; ok {
		return d
	}
	if zip[0] == '9' && (zip[1] == '7' || zip[1] == '8') {
		return zip[:3]
	}
	return zip[:2]
}

// isFiveDigitZip reports whether zip is exactly five ASCII digits.
func isFiveDigitZip(zip string) bool {
	if len(zip) != 5 {
		return false
	}
	for i := range len(zip) {
		if zip[i] < '0' || zip[i] > '9' {
			return false
		}
	}
	return true
}
