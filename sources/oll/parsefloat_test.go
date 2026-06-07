package oll

import "testing"

// parseFrenchFloat strips French digit-group spaces (regular U+0020, no-break
// U+00A0, narrow no-break U+202F) and accepts a comma decimal — the OLL exports
// use all three.
func TestParseFrenchFloat(t *testing.T) {
	ok := []struct {
		in   string
		want float64
	}{
		{"16,4", 16.4},
		{"1 234,5", 1234.5}, // regular space U+0020
		{"1 234,5", 1234.5}, // no-break space U+00A0
		{"1 234,5", 1234.5}, // narrow no-break space U+202F
		{" 12.5 ", 12.5},    // a dot is also accepted
		{"0", 0},
	}
	for _, c := range ok {
		got, k := parseFrenchFloat(c.in)
		if !k || got != c.want {
			t.Errorf("parseFrenchFloat(%q) = %v, %v; want %v, true", c.in, got, k, c.want)
		}
	}
	for _, bad := range []string{"", "   ", "N/A", "abc"} {
		if _, k := parseFrenchFloat(bad); k {
			t.Errorf("parseFrenchFloat(%q): want ok=false", bad)
		}
	}
}

// FuzzParseRents asserts the upstream-CSV row parser never panics on arbitrary
// input — ragged rows, missing columns, weird encodings — exercising the
// readCSV → headerIndex → field → parseFrenchFloat pipeline.
func FuzzParseRents(f *testing.F) {
	f.Add("zone_calcul;type_habitat;nombre_pieces_local;nombre_pieces_homogene;" +
		"epoque_construction_local;epoque_construction_homogene;" +
		"anciennete_locataire_local;anciennete_locataire_homogene;" +
		"loyer_median;loyer_1_quartile;loyer_3_quartile;surface_moyenne;nombre_observations\n" +
		"L75.4.05;Appartement;;Appart 2P;;;;;16,4;14,0;19,0;45,0;120\n")
	f.Add("")
	f.Add("a;b\nc;d\n")
	f.Fuzz(func(t *testing.T, text string) {
		_, _ = parseRents(text) // must not panic on arbitrary input
	})
}
