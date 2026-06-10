package communes

import "strings"

// DeptFromZip extracts the département code from a 5-digit French postal
// code: 2-digit for métropole, 3-digit for DOM-TOM (97x/98x). Corsican
// zips have no exact postal↔dept mapping: 200xx/201xx default to "2A"
// (Corse-du-Sud), 202xx+ to "2B" (Haute-Corse) — the boundary is fuzzy,
// callers that match against it should retry the other half on a miss
// (ResolveINSEE does). Returns "" for malformed input.
//
// (helpers/banx.DeptFromZip is a delegating alias kept for its existing
// callers; this is the canonical home.)
func DeptFromZip(zip string) string {
	zip = strings.TrimSpace(zip)
	if len(zip) != 5 {
		return ""
	}
	for i := range 5 {
		if zip[i] < '0' || zip[i] > '9' {
			return ""
		}
	}
	switch zip[:2] {
	case "20":
		if zip[2] == '0' || zip[2] == '1' {
			return "2A"
		}
		return "2B"
	case "97", "98":
		return zip[:3]
	default:
		return zip[:2]
	}
}

// ResolveINSEE resolves a (city, zip) pair to a 5-digit INSEE code fully
// offline — no BAN round-trip — using the embedded commune table plus
// the Paris/Lyon/Marseille postal-code conventions:
//
//   - Paris: zip 75001..75020 (and the 75116 exception) map directly to
//     the arrondissement INSEE 75101..75120 / 75116.
//   - Lyon: zip 69001..69009 → INSEE 69381..69389.
//   - Marseille: zip 13001..13016 → INSEE 13201..13216.
//   - Everywhere else: (normalized city name, département-from-zip)
//     against the table, retrying the other Corsican département when
//     the fuzzy 2A/2B zip split guessed wrong.
//
// ok is false when nothing matches (unknown city, malformed zip). For
// free-text addresses without a clean (city, zip) pair, use the live
// banx.INSEEResolver cascade instead — this helper is for callers that
// already hold structured fields and want zero network I/O.
func (t *Table) ResolveINSEE(city, zip string) (string, bool) {
	zip = strings.TrimSpace(zip)
	city = strings.TrimSpace(city)
	if len(zip) != 5 {
		return "", false
	}

	// Paris arrondissements. 75116 (Paris 16e "bis" postal code) is the
	// lone irregular: it maps to its own INSEE 75116.
	if strings.HasPrefix(zip, "75") {
		arr := zip[2:]
		if arr == "116" {
			return "75116", true
		}
		if arr[0] == '0' {
			n := int(arr[1]-'0')*10 + int(arr[2]-'0')
			if n >= 1 && n <= 20 {
				return "751" + arr[1:], true
			}
		}
	}
	// Lyon arrondissements: 69001..69009 → 69381..69389.
	if strings.HasPrefix(zip, "6900") && zip[4] >= '1' && zip[4] <= '9' {
		return "6938" + zip[4:], true
	}
	// Marseille arrondissements: 13001..13016 → 13201..13216.
	if strings.HasPrefix(zip, "130") {
		n := int(zip[3]-'0')*10 + int(zip[4]-'0')
		if n >= 1 && n <= 16 {
			return "132" + zip[3:], true
		}
	}

	if t == nil || city == "" {
		return "", false
	}
	dept := DeptFromZip(zip)
	if dept == "" {
		return "", false
	}
	if insee, ok := t.INSEEByCityDept(city, dept); ok {
		return insee, true
	}
	// Corsica boundary fuzz: the 2A/2B zip split is approximate, so a
	// miss on one half retries the other.
	switch dept {
	case "2A":
		if insee, ok := t.INSEEByCityDept(city, "2B"); ok {
			return insee, true
		}
	case "2B":
		if insee, ok := t.INSEEByCityDept(city, "2A"); ok {
			return insee, true
		}
	}
	return "", false
}
