package oll

import "testing"

// FuzzParseRents asserts the upstream-CSV row parser never panics on arbitrary
// input — ragged rows, missing columns, weird encodings — exercising the
// readCSV → headerIndex → field → frnorm.ParseFRFloat pipeline.
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
