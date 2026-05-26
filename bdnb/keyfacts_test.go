package bdnb

import "testing"

func TestExtractBuildingYear_PrefersDPE(t *testing.T) {
	result := map[string]any{
		"building": map[string]any{
			"annee_construction":     float64(1900),
			"annee_construction_dpe": float64(1975),
		},
	}
	got, ok := ExtractBuildingYear(result)
	if !ok || got != 1975 {
		t.Errorf("ExtractBuildingYear = (%d, %v), want (1975, true)", got, ok)
	}
}

func TestExtractBuildingYear_FallsBackToCadastre(t *testing.T) {
	result := map[string]any{
		"building": map[string]any{
			"annee_construction": float64(1885),
		},
	}
	got, ok := ExtractBuildingYear(result)
	if !ok || got != 1885 {
		t.Errorf("ExtractBuildingYear = (%d, %v), want (1885, true)", got, ok)
	}
}

func TestExtractBuildingYear_AbsentOrZero(t *testing.T) {
	cases := []map[string]any{
		nil,
		{"building": map[string]any{}},
		{"building": map[string]any{"annee_construction": float64(0)}},
		{"building": "not a map"},
	}
	for i, c := range cases {
		if _, ok := ExtractBuildingYear(c); ok {
			t.Errorf("case %d: expected ok=false", i)
		}
	}
}

func TestExtractBuildingFloors(t *testing.T) {
	result := map[string]any{"building": map[string]any{"nb_niveau": float64(4)}}
	got, ok := ExtractBuildingFloors(result)
	if !ok || got != 4 {
		t.Errorf("ExtractBuildingFloors = (%d, %v), want (4, true)", got, ok)
	}
}

func TestExtractDwellingCount(t *testing.T) {
	result := map[string]any{"building": map[string]any{"nb_log": float64(28)}}
	got, ok := ExtractDwellingCount(result)
	if !ok || got != 28 {
		t.Errorf("ExtractDwellingCount = (%d, %v), want (28, true)", got, ok)
	}
}

func TestExtractBuildingDPEClass(t *testing.T) {
	cases := []struct {
		in     string
		wantOk bool
		want   string
	}{
		{"G", true, "G"},
		{"a", true, "A"},
		{"  D  ", true, "D"},
		{"H", false, ""},
		{"", false, ""},
		{"AB", false, ""},
	}
	for _, c := range cases {
		result := map[string]any{"dpe": map[string]any{"classe_bilan": c.in}}
		got, ok := ExtractBuildingDPEClass(result)
		if ok != c.wantOk || got != c.want {
			t.Errorf("DPE class %q = (%q, %v), want (%q, %v)", c.in, got, ok, c.want, c.wantOk)
		}
	}
}

func TestExtractMonumentDistanceM(t *testing.T) {
	result := map[string]any{"risks": map[string]any{"monument_historique_m": float64(180)}}
	got, ok := ExtractMonumentDistanceM(result)
	if !ok || got != 180 {
		t.Errorf("ExtractMonumentDistanceM = (%d, %v), want (180, true)", got, ok)
	}
}

func TestExtractQuartierPrioritaire(t *testing.T) {
	cases := []struct {
		raw    any
		wantOk bool
		want   bool
	}{
		{"oui", true, true},
		{"OUI", true, true},
		{"non", true, false},
		{"NO", true, false},
		{"true", true, true},
		{"0", true, false},
		{"1", true, true},
		{true, true, true},
		{false, true, false},
		{"", false, false},
		{nil, false, false},
		{"maybe", false, false},
	}
	for _, c := range cases {
		result := map[string]any{"risks": map[string]any{"quartier_prioritaire": c.raw}}
		got, ok := ExtractQuartierPrioritaire(result)
		if ok != c.wantOk || got != c.want {
			t.Errorf("QP %v = (%v, %v), want (%v, %v)", c.raw, got, ok, c.want, c.wantOk)
		}
	}
}

func TestExtractBuildingHeightM(t *testing.T) {
	result := map[string]any{"building": map[string]any{"hauteur_mean_m": float64(12)}}
	got, ok := ExtractBuildingHeightM(result)
	if !ok || got != 12 {
		t.Errorf("ExtractBuildingHeightM = (%d, %v), want (12, true)", got, ok)
	}
}

func TestExtractABFPerimeter(t *testing.T) {
	cases := []struct {
		risks  map[string]any
		want   bool
		wantOk bool
	}{
		{map[string]any{"perimetre_mh": float64(1), "contrainte_abf_ac1": float64(0)}, true, true},
		{map[string]any{"perimetre_mh": float64(0), "contrainte_abf_ac1": float64(1)}, true, true},
		{map[string]any{"perimetre_mh": float64(1), "contrainte_abf_ac1": float64(1)}, true, true},
		{map[string]any{"perimetre_mh": float64(0), "contrainte_abf_ac1": float64(0)}, false, true},
		{map[string]any{"perimetre_mh": true}, true, true},
		{map[string]any{"contrainte_abf_ac1": "oui"}, true, true},
		{map[string]any{}, false, false},
		{nil, false, false},
	}
	for i, c := range cases {
		result := map[string]any{}
		if c.risks != nil {
			result["risks"] = c.risks
		}
		got, ok := ExtractABFPerimeter(result)
		if got != c.want || ok != c.wantOk {
			t.Errorf("case %d (risks=%v): got (%v, %v), want (%v, %v)", i, c.risks, got, ok, c.want, c.wantOk)
		}
	}
}

func TestExtractPLUBatiPatrimonial(t *testing.T) {
	if got, ok := ExtractPLUBatiPatrimonial(map[string]any{"risks": map[string]any{"zone_plu_bati_patrimonial": float64(1)}}); !ok || !got {
		t.Errorf("PLU=1: got (%v, %v), want (true, true)", got, ok)
	}
	if got, ok := ExtractPLUBatiPatrimonial(map[string]any{"risks": map[string]any{"zone_plu_bati_patrimonial": float64(0)}}); !ok || got {
		t.Errorf("PLU=0: got (%v, %v), want (false, true)", got, ok)
	}
	if _, ok := ExtractPLUBatiPatrimonial(map[string]any{"risks": map[string]any{}}); ok {
		t.Errorf("PLU absent: ok=true, want false")
	}
}

func TestExtractUsagePrincipal(t *testing.T) {
	cases := []struct {
		building map[string]any
		want     string
		wantOk   bool
	}{
		{map[string]any{"usage_principal": "Résidentiel collectif"}, "Résidentiel collectif", true},
		{map[string]any{"usage_principal": "  Tertiaire  "}, "Tertiaire", true},
		{map[string]any{"usage_principal": ""}, "", false},
		{map[string]any{}, "", false},
		{nil, "", false},
	}
	for i, c := range cases {
		result := map[string]any{}
		if c.building != nil {
			result["building"] = c.building
		}
		got, ok := ExtractUsagePrincipal(result)
		if got != c.want || ok != c.wantOk {
			t.Errorf("case %d: got (%q, %v), want (%q, %v)", i, got, ok, c.want, c.wantOk)
		}
	}
}
