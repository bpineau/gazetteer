package communes

import "testing"

func TestDeptFromZip(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"75011": "75",
		"93100": "93",
		"20000": "2A", // Ajaccio
		"20200": "2B", // Bastia
		"97400": "974",
		"98800": "988",
		"7501":  "",
		"7501x": "",
		"":      "",
	}
	for zip, want := range cases {
		if got := DeptFromZip(zip); got != want {
			t.Errorf("DeptFromZip(%q) = %q, want %q", zip, got, want)
		}
	}
}

func TestResolveINSEE(t *testing.T) {
	t.Parallel()
	table := MustDefault()

	cases := []struct {
		name, city, zip, want string
	}{
		{"paris_arrondissement", "Paris", "75011", "75111"},
		{"paris_16e_bis", "Paris", "75116", "75116"},
		{"lyon_arrondissement", "Lyon", "69003", "69383"},
		{"marseille_arrondissement", "Marseille", "13008", "13208"},
		{"plain_commune", "Montreuil", "93100", "93048"},
		{"city_dept_disambiguation", "Saint-Denis", "93200", "93066"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := table.ResolveINSEE(c.city, c.zip)
			if !ok || got != c.want {
				t.Errorf("ResolveINSEE(%q, %q) = (%q, %v), want %q", c.city, c.zip, got, ok, c.want)
			}
		})
	}

	// PLM rules fire on zip alone, even with an empty city.
	if got, ok := table.ResolveINSEE("", "75005"); !ok || got != "75105" {
		t.Errorf("zip-only Paris = (%q, %v), want 75105", got, ok)
	}

	// Corsican retry: Bastia is 2B but a 201xx zip guesses 2A first.
	if got, ok := table.ResolveINSEE("Bastia", "20200"); !ok || got != "2B033" {
		t.Errorf("Bastia = (%q, %v), want 2B033", got, ok)
	}

	// Misses.
	if _, ok := table.ResolveINSEE("Nowhereville", "93100"); ok {
		t.Error("unknown city must miss")
	}
	if _, ok := table.ResolveINSEE("Paris", "750"); ok {
		t.Error("malformed zip must miss")
	}
	var nilTable *Table
	if _, ok := nilTable.ResolveINSEE("Montreuil", "93100"); ok {
		t.Error("nil table must miss for non-PLM zips")
	}
}
