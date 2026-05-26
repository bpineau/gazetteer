// keyfacts.go — pure helper transforms from the BDNB result payload
// (as decoded into a map[string]any from EnrichPayload.Result) into the
// few "key facts" callers want to surface front-and-center on a detail
// page.
//
// These functions DO NOT touch the parser, the fetcher, or the result
// blob shape — they read the already-marshalled JSON tree. Adding a new
// key fact never bumps any persisted-payload schema version: it's a
// pure projection over data that is already being persisted.
//
// All extractors return `(value, ok)`: `ok==false` means the field was
// absent, null, or of the wrong type (defensive parsing — never panic
// on a malformed payload).
package bdnb

import "strings"

// ExtractBuildingYear returns the most reliable year of construction
// available in the BDNB result blob. Preference order:
//
//  1. building.annee_construction_dpe (the year reported on the DPE,
//     usually authoritative for residential buildings) ;
//  2. building.annee_construction (the BDNB-derived year, may be empty
//     for historic buildings).
//
// Returns (0, false) when neither is set or both are non-positive.
func ExtractBuildingYear(result map[string]any) (int, bool) {
	building, _ := result["building"].(map[string]any)
	if building == nil {
		return 0, false
	}
	if v, ok := numField(building, "annee_construction_dpe"); ok && v > 0 {
		return int(v), true
	}
	if v, ok := numField(building, "annee_construction"); ok && v > 0 {
		return int(v), true
	}
	return 0, false
}

// ExtractBuildingFloors returns the number of above-ground floors
// (`nb_niveau`). 0 → ground-floor-only is a meaningful value, so we
// only treat negative / missing as "unknown".
func ExtractBuildingFloors(result map[string]any) (int, bool) {
	building, _ := result["building"].(map[string]any)
	if building == nil {
		return 0, false
	}
	v, ok := numField(building, "nb_niveau")
	if !ok || v < 0 {
		return 0, false
	}
	return int(v), true
}

// ExtractBuildingHeightM returns the mean building height in metres
// (`hauteur_mean_m`). When floors are unknown but height is set, this
// is a useful proxy ("4-storey ≈ 12 m").
func ExtractBuildingHeightM(result map[string]any) (int, bool) {
	building, _ := result["building"].(map[string]any)
	if building == nil {
		return 0, false
	}
	v, ok := numField(building, "hauteur_mean_m")
	if !ok || v <= 0 {
		return 0, false
	}
	return int(v), true
}

// ExtractDwellingCount returns the number of dwellings in the building
// (`nb_log`). A value of 1 typically means a single-family house.
func ExtractDwellingCount(result map[string]any) (int, bool) {
	building, _ := result["building"].(map[string]any)
	if building == nil {
		return 0, false
	}
	v, ok := numField(building, "nb_log")
	if !ok || v <= 0 {
		return 0, false
	}
	return int(v), true
}

// ExtractBuildingDPEClass returns the DPE class as the BDNB rolls it up
// at the BUILDING level (`dpe.classe_bilan`). This is the bilan-globale
// class — a building can have a different class from the auctioned unit
// itself (an A unit on top of a G building, for instance). Returns
// (uppercase letter, true) for valid A..G; ("", false) otherwise.
func ExtractBuildingDPEClass(result map[string]any) (string, bool) {
	dpe, _ := result["dpe"].(map[string]any)
	if dpe == nil {
		return "", false
	}
	cls, _ := dpe["classe_bilan"].(string)
	cls = strings.TrimSpace(strings.ToUpper(cls))
	if len(cls) != 1 {
		return "", false
	}
	switch cls[0] {
	case 'A', 'B', 'C', 'D', 'E', 'F', 'G':
		return cls, true
	}
	return "", false
}

// ExtractMonumentDistanceM returns the distance in metres to the
// nearest historical monument (`risks.monument_historique_m`). BDNB
// caps this at a few-km radius; absent → not in scope.
func ExtractMonumentDistanceM(result map[string]any) (int, bool) {
	risks, _ := result["risks"].(map[string]any)
	if risks == nil {
		return 0, false
	}
	v, ok := numField(risks, "monument_historique_m")
	if !ok || v <= 0 {
		return 0, false
	}
	return int(v), true
}

// ExtractQuartierPrioritaire returns true when the BDNB tags the
// building's address as part of a Quartier Prioritaire de la
// Politique de la Ville (QPV). The field comes through as a string on
// BDNB ("oui" / "non" / "" / "1" / "0") so we normalize.
//
// Returns (true, true) for "oui"/"yes"/"true"/"1"; (false, true) for
// "non"/"no"/"false"/"0"; (_, false) for unknown / absent.
func ExtractQuartierPrioritaire(result map[string]any) (bool, bool) {
	risks, _ := result["risks"].(map[string]any)
	if risks == nil {
		return false, false
	}
	raw, ok := risks["quartier_prioritaire"]
	if !ok || raw == nil {
		return false, false
	}
	switch v := raw.(type) {
	case bool:
		return v, true
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		switch s {
		case "":
			return false, false
		case "oui", "yes", "true", "1":
			return true, true
		case "non", "no", "false", "0":
			return false, true
		}
	}
	return false, false
}

// ExtractABFPerimeter reports whether the BDNB tags the building as
// being inside a Bâtiments-de-France perimeter (`risks.perimetre_mh`
// or `risks.contrainte_abf_ac1`). Either flag set → in-scope (ABF
// review required for facade / roof works, which dramatically
// changes the renovation budget). Returns (true, true) when AT
// LEAST one of the two flags is set; (false, true) when BOTH are
// explicitly cleared; (_, false) when neither field is present.
func ExtractABFPerimeter(result map[string]any) (bool, bool) {
	risks, _ := result["risks"].(map[string]any)
	if risks == nil {
		return false, false
	}
	mh, mhOK := flagField(risks, "perimetre_mh")
	abf, abfOK := flagField(risks, "contrainte_abf_ac1")
	if !mhOK && !abfOK {
		return false, false
	}
	return mh || abf, true
}

// ExtractPLUBatiPatrimonial reports whether the building falls in a
// PLU "patrimonial" zone (`risks.zone_plu_bati_patrimonial`) — a
// communal-level restriction that constrains demolition + facade
// modifications, similar in spirit to but separate from ABF.
// Returns (value, true) when the field is present; (_, false) when
// absent.
func ExtractPLUBatiPatrimonial(result map[string]any) (bool, bool) {
	risks, _ := result["risks"].(map[string]any)
	if risks == nil {
		return false, false
	}
	return flagField(risks, "zone_plu_bati_patrimonial")
}

// ExtractUsagePrincipal returns the building's principal usage
// (`building.usage_principal`) — typically "Résidentiel individuel" /
// "Résidentiel collectif" / "Tertiaire" / "Industriel". Empty /
// missing → not surfaced.
func ExtractUsagePrincipal(result map[string]any) (string, bool) {
	building, _ := result["building"].(map[string]any)
	if building == nil {
		return "", false
	}
	v, ok := building["usage_principal"].(string)
	if !ok {
		return "", false
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return "", false
	}
	return v, true
}

// flagField is the BDNB "boolean-ish" coercer used by the risk-flag
// extractors. BDNB serialises booleans as either real JSON booleans,
// numeric 0/1, or the strings "oui"/"non"/"true"/"false"/"1"/"0".
// Returns (value, true) on a recognised shape; (_, false) on absent
// or unrecognised.
func flagField(m map[string]any, key string) (bool, bool) {
	if m == nil {
		return false, false
	}
	raw, ok := m[key]
	if !ok || raw == nil {
		return false, false
	}
	switch v := raw.(type) {
	case bool:
		return v, true
	case float64:
		return v != 0, true
	case int:
		return v != 0, true
	case int64:
		return v != 0, true
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "":
			return false, false
		case "oui", "yes", "true", "1":
			return true, true
		case "non", "no", "false", "0":
			return false, true
		}
	}
	return false, false
}

// numField is a defensive numeric reader for map[string]any payloads.
// Local copy so the gazetteer package stays dependency-free.
func numField(m map[string]any, key string) (float64, bool) {
	if m == nil {
		return 0, false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}
