package carteloyers

import (
	"strings"
	"testing"
)

const clHeader = "INSEE_C;DEP;loypredm2;lwr_IPm2;upr_IPm2;TYPPRED;nbobs_com"

// parseCSV must reject structurally broken input (so a bad refresh/embed fails
// loud) and silently skip rows with unparseable rents rather than abort.
func TestParseCSV_Negative(t *testing.T) {
	cases := []struct {
		name     string
		csv      string
		wantErr  bool
		wantRows int
	}{
		{"empty input", "", true, 0},
		{"header only", clHeader + "\n", false, 0},
		{"missing required column", "INSEE_C;DEP;loypredm2;lwr_IPm2;upr_IPm2;TYPPRED\n", true, 0},
		{"ragged row", clHeader + "\n75056;75;1\n", true, 0},
		{"good row", clHeader + "\n75056;75;12,5;10,0;15,0;Appartement;100\n", false, 1},
		{"unparseable rent skipped", clHeader + "\n75056;75;abc;10,0;15,0;Appartement;100\n", false, 0},
		{"blank INSEE skipped", clHeader + "\n;75;12,5;10,0;15,0;Appartement;100\n", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m, err := parseCSV(strings.NewReader(tc.csv))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil (rows=%d)", len(m))
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(m) != tc.wantRows {
				t.Fatalf("rows=%d, want %d", len(m), tc.wantRows)
			}
		})
	}

	// A good row maps to the typed Row with the comma-decimal rents parsed.
	m, err := parseCSV(strings.NewReader(clHeader + "\n75056;75;12,5;10,0;15,0;Appartement;100\n"))
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	r, ok := m["75056"]
	if !ok || r.LoyerMedCC != 12.5 || r.LoyerLowerCC != 10.0 || r.LoyerUpperCC != 15.0 || r.NbObsCommune != 100 || r.Department != "75" {
		t.Fatalf("bad Row: %+v (ok=%v)", r, ok)
	}
}

func TestParseCommaFloat(t *testing.T) {
	ok := []struct {
		in   string
		want float64
	}{
		{"9,75769", 9.75769},
		{" 1,5 ", 1.5},
		{"12.5", 12.5}, // a dot is also accepted
		{"0", 0},
	}
	for _, c := range ok {
		got, err := parseCommaFloat(c.in)
		if err != nil || got != c.want {
			t.Errorf("parseCommaFloat(%q) = %v, %v; want %v, nil", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{"", "   ", "abc", "1,2,3"} {
		if _, err := parseCommaFloat(bad); err == nil {
			t.Errorf("parseCommaFloat(%q): want error", bad)
		}
	}
}

// FuzzParseCSV asserts the parser never panics and that a nil error yields a
// non-nil map.
func FuzzParseCSV(f *testing.F) {
	f.Add(clHeader + "\n75056;75;12,5;10,0;15,0;Appartement;100\n")
	f.Add("")
	f.Add(clHeader + "\n")
	f.Fuzz(func(t *testing.T, data string) {
		m, err := parseCSV(strings.NewReader(data))
		if err == nil && m == nil {
			t.Fatal("nil map with nil error")
		}
	})
}
