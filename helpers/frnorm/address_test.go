package frnorm

import "testing"

// TestExtractZipFromAddress locks the simple "first 5-digit token" rule
// shared by every scraper. DOM-TOM (97x/98x) zips are accepted. Inputs
// without any 5-digit token return ("", false).
func TestExtractZipFromAddress(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantZip string
		wantOK  bool
	}{
		{"empty", "", "", false},
		{"no zip", "rue de la Paix", "", false},
		{"plain zip", "75011 Paris", "75011", true},
		{"zip in middle", "Paris 75011, France", "75011", true},
		{"4-digit rejected", "1234 Paris", "", false},
		{"6-digit rejected", "750110 Paris", "", false},
		{"first match wins", "see 75001 then 75011", "75001", true},
		{"trailing punct ok", "(75011) Paris", "75011", true},
		{"DOM-TOM Guadeloupe", "97110 Pointe-à-Pitre", "97110", true},
		{"DOM-TOM Reunion", "97400 Saint-Denis", "97400", true},
		{"DOM-TOM Mayotte", "97600 Mamoudzou", "97600", true},
		{"DOM-TOM Saint-Martin", "97150 Saint-Martin", "97150", true},
		{"low zip 01", "01000 Bourg-en-Bresse", "01000", true},
		{"high zip 95", "95000 Cergy", "95000", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ExtractZipFromAddress(c.in)
			if got != c.wantZip || ok != c.wantOK {
				t.Errorf("ExtractZipFromAddress(%q) = (%q, %v), want (%q, %v)",
					c.in, got, ok, c.wantZip, c.wantOK)
			}
		})
	}
}

// TestExtractZipCity locks the canonical trailing-pattern semantics
// promoted from avoventes. Sources facing mid-address zips (e.g. lawyer
// cabinets) layer their own city heuristic on top of
// ExtractZipFromAddress and don't go through ExtractZipCity.
func TestExtractZipCity(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantZip  string
		wantCity string
		wantOK   bool
	}{
		{
			name: "empty", in: "",
			wantZip: "", wantCity: "", wantOK: false,
		},
		{
			name: "trailing France", in: "3 bis Av. du Président John Kennedy, 93110 Rosny-sous-Bois, France",
			wantZip: "93110", wantCity: "Rosny-sous-Bois", wantOK: true,
		},
		{
			name: "no France suffix", in: "4 Imp. Gantz, 69008 Lyon",
			wantZip: "69008", wantCity: "Lyon", wantOK: true,
		},
		{
			name: "leading prose", in: "foo, 75011 Paris",
			wantZip: "75011", wantCity: "Paris", wantOK: true,
		},
		{
			name: "lowercase city", in: "12 rue truc, 75011 paris",
			wantZip: "75011", wantCity: "paris", wantOK: true,
		},
		{
			name: "Paris arrondissement word form", in: "1 rue de Rivoli, 75001 Paris 1er",
			wantZip: "75001", wantCity: "Paris 1er", wantOK: true,
		},
		{
			name: "Paris arrondissement abbr 11e", in: "10 rue Oberkampf, 75011 Paris 11e",
			wantZip: "75011", wantCity: "Paris 11e", wantOK: true,
		},
		{
			name: "trailing comma after city before France", in: "8 rue Foo, 31000 Toulouse, France",
			wantZip: "31000", wantCity: "Toulouse", wantOK: true,
		},
		{
			name: "no zip", in: "rue de la Paix, Paris",
			wantZip: "", wantCity: "", wantOK: false,
		},
		{
			name: "zip with prose tail (no comma)", in: "75011 then more text",
			wantZip: "75011", wantCity: "then more text", wantOK: true,
		},
		{
			name: "double-barrelled city", in: "rue X, 92800 Puteaux-la-Défense",
			wantZip: "92800", wantCity: "Puteaux-la-Défense", wantOK: true,
		},
		{
			name: "city with apostrophe", in: "12 rue Y, 95000 Cergy-l'Étoile",
			wantZip: "95000", wantCity: "Cergy-l'Étoile", wantOK: true,
		},
		{
			name: "DOM-TOM Guadeloupe", in: "rue X, 97110 Pointe-à-Pitre",
			wantZip: "97110", wantCity: "Pointe-à-Pitre", wantOK: true,
		},
		{
			name: "DOM-TOM Reunion with France", in: "rue Y, 97400 Saint-Denis, France",
			wantZip: "97400", wantCity: "Saint-Denis", wantOK: true,
		},
		{
			name: "DOM-TOM Mayotte", in: "12 rue Z, 97600 Mamoudzou",
			wantZip: "97600", wantCity: "Mamoudzou", wantOK: true,
		},
		{
			name: "uppercase city", in: "12 rue Z, 92200 NEUILLY-SUR-SEINE",
			wantZip: "92200", wantCity: "NEUILLY-SUR-SEINE", wantOK: true,
		},
		{
			name: "low dept 01", in: "rue X, 01000 Bourg-en-Bresse",
			wantZip: "01000", wantCity: "Bourg-en-Bresse", wantOK: true,
		},
		{
			name: "high dept 95", in: "rue X, 95000 Cergy",
			wantZip: "95000", wantCity: "Cergy", wantOK: true,
		},
		{
			name: "long dept 974", in: "8 rue de la Mer, 97410 Saint-Pierre",
			wantZip: "97410", wantCity: "Saint-Pierre", wantOK: true,
		},
		{
			name: "trailing whitespace", in: "12 rue X, 75011 Paris   ",
			wantZip: "75011", wantCity: "Paris", wantOK: true,
		},
		{
			name: "single token city", in: "75001 Paris",
			wantZip: "75001", wantCity: "Paris", wantOK: true,
		},
		{
			name: "city with accent", in: "rue X, 49000 Angers-sur-Évre",
			wantZip: "49000", wantCity: "Angers-sur-Évre", wantOK: true,
		},
		{
			name: "trailing france lowercase", in: "rue X, 75011 Paris, france",
			// The trailing "France" suffix in the regex is case-sensitive
			// (matches the 4 production scrapers' actual data shape) ; a
			// lower-cased "france" defeats the optional alternative and the
			// commas force the regex to fail because `[^,]+?` cannot span
			// past the second comma.
			wantZip: "", wantCity: "", wantOK: false,
		},
		{
			name: "no city after zip", in: "see 75011",
			wantZip: "", wantCity: "", wantOK: false,
		},
		{
			name: "two zips first valid match wins",
			// Go regex is leftmost-first ; the first 5-digit token whose
			// tail satisfies the non-greedy [^,]+? clause wins. Documents
			// the deterministic resolution.
			in:      "first 75001 then ends 13002 Marseille",
			wantZip: "75001", wantCity: "then ends 13002 Marseille", wantOK: true,
		},
		{
			name: "city is just France label", in: "rue X, 75001 France",
			wantZip: "75001", wantCity: "France", wantOK: true, // accepted: city literally "France" preserved
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotZip, gotCity, gotOK := ExtractZipCity(c.in)
			if gotZip != c.wantZip || gotCity != c.wantCity || gotOK != c.wantOK {
				t.Errorf("ExtractZipCity(%q) = (%q, %q, %v), want (%q, %q, %v)",
					c.in, gotZip, gotCity, gotOK, c.wantZip, c.wantCity, c.wantOK)
			}
		})
	}
}
