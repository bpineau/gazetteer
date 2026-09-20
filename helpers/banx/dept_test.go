package banx

import "testing"

// TestDeptFromZip covers the canonical zip → département encoding
// shared by every consumer keyed on département (BAN dept-guard,
// commune lookups). The cases exercise the four branches: 2-digit
// métropole, Corsica 2A/2B split, DOM-TOM 3-digit, rejection of
// malformed input.
func TestDeptFromZip(t *testing.T) {
	cases := []struct {
		name string
		zip  string
		want string
	}{
		// Métropolitain — 2-digit dept.
		{"paris_11", "75011", "75"},
		{"paris_centre", "75001", "75"},
		{"vannes", "56000", "56"},
		{"yvelines", "78210", "78"},
		{"gironde", "33000", "33"},

		// Corsica — 20XXX splits between 2A and 2B.
		{"ajaccio_20000", "20000", "2A"},
		{"sartene_20100", "20100", "2A"},
		{"bastia_20200", "20200", "2B"},
		{"biguglia_20620", "20620", "2B"},

		// DOM-TOM — 3-digit dept.
		{"guadeloupe", "97100", "971"},
		{"martinique", "97200", "972"},
		{"guyane", "97300", "973"},
		{"reunion", "97400", "974"},
		{"mayotte", "97600", "976"},
		// 98xxx covers Saint-Pierre-et-Miquelon, Polynésie française,
		// Wallis-et-Futuna, Nouvelle-Calédonie.
		{"polynesie_98700", "98700", "987"},
		{"nouvelle_caledonie", "98800", "988"},

		// Whitespace-tolerant.
		{"whitespace", " 75011 ", "75"},

		// Rejections.
		{"too_short", "7501", ""},
		{"too_long", "750110", ""},
		{"alpha", "abcde", ""},
		{"mixed", "750AB", ""},
		{"empty", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DeptFromZip(c.zip); got != c.want {
				t.Errorf("DeptFromZip(%q) = %q, want %q", c.zip, got, c.want)
			}
		})
	}
}

// TestDeptMatchKey_CrossCorsica documents the deliberate divergence
// from DeptFromZip: the match key folds both Corsican departments
// under a single "20" prefix so cross-island zips still pair up during
// comparable selection. Tightening the Corsica split here would break
// BAN-cache and downstream fuzzy-resolver picker semantics.
func TestDeptMatchKey_CrossCorsica(t *testing.T) {
	if deptMatchKey("20000") != deptMatchKey("20200") {
		t.Errorf("deptMatchKey should fold 2A and 2B under the same key, got %q vs %q",
			deptMatchKey("20000"), deptMatchKey("20200"))
	}
}

// TestZipsShareDepartment_CollectivitesAndMalformed pins the two cases a bare
// prefix gets wrong: the collectivités that kept a 971xx postal code, and a
// zip whose leading zero was eaten upstream.
func TestZipsShareDepartment_CollectivitesAndMalformed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want bool
		why  string
	}{
		{"97133", "97190", false, "Saint-Barthélemy (977) vs Le Gosier, Guadeloupe (971)"},
		{"97150", "97110", false, "Saint-Martin (978) vs Pointe-à-Pitre, Guadeloupe (971)"},
		{"97133", "97150", false, "Saint-Barthélemy vs Saint-Martin, two collectivités"},
		{"97133", "97133", true, "same zip"},
		{"97110", "97190", true, "both Guadeloupe"},
		{"1000", "10000", false, "Bourg-en-Bresse unpadded (01) vs Troyes (10)"},
		{"1000", "1000", true, "malformed but equal"},
		{"750", "75001", false, "truncated zip must not fold onto Paris"},
		{"75001", "75020", true, "both Paris"},
		{"20000", "20600", true, "Corsica folds to one key by design"},
		{"97400", "97410", true, "both Réunion"},
		{"97400", "97300", false, "Réunion vs Guyane"},
	}
	for _, c := range cases {
		if got := ZipsShareDepartment(c.a, c.b); got != c.want {
			t.Errorf("ZipsShareDepartment(%q, %q) = %v, want %v — %s", c.a, c.b, got, c.want, c.why)
		}
	}
}
