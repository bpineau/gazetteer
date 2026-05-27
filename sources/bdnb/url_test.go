package bdnb

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestURLForBANID_Happy(t *testing.T) {
	t.Parallel()

	got, err := URLForBANID("75111", "75111_6507_00003")
	if err != nil {
		t.Fatalf("URLForBANID: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	if u.Path != "/v1/bdnb/donnees/batiment_groupe_complet" {
		t.Errorf("path = %q", u.Path)
	}
	q := u.Query()
	if q.Get("code_commune_insee") != "eq.75111" {
		t.Errorf("code_commune_insee = %q", q.Get("code_commune_insee"))
	}
	if q.Get("cle_interop_adr_principale_ban") != "eq.75111_6507_00003" {
		t.Errorf("cle_interop_adr_principale_ban = %q", q.Get("cle_interop_adr_principale_ban"))
	}
	if !strings.Contains(q.Get("select"), "batiment_groupe_id") {
		t.Errorf("select missing batiment_groupe_id: %q", q.Get("select"))
	}
	if q.Get("limit") != "5" {
		t.Errorf("limit = %q", q.Get("limit"))
	}
}

func TestURLForBANID_MissingInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		insee, ban string
	}{
		{"", "abc"},
		{"75111", ""},
		{"  ", "  "},
	}
	for _, tc := range tests {
		_, err := URLForBANID(tc.insee, tc.ban)
		if !errors.Is(err, ErrInsufficientFilter) {
			t.Errorf("URLForBANID(%q,%q) = %v, want ErrInsufficientFilter", tc.insee, tc.ban, err)
		}
	}
}

func TestURLForAddress_Happy(t *testing.T) {
	t.Parallel()

	got, err := URLForAddress("75111", "Voltaire")
	if err != nil {
		t.Fatalf("URLForAddress: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	q := u.Query()
	if q.Get("code_commune_insee") != "eq.75111" {
		t.Errorf("code_commune_insee = %q", q.Get("code_commune_insee"))
	}
	if q.Get("libelle_adr_principale_ban") != "ilike.*Voltaire*" {
		t.Errorf("libelle_adr_principale_ban = %q", q.Get("libelle_adr_principale_ban"))
	}
}

func TestURLForAddress_PreservesExplicitWildcard(t *testing.T) {
	t.Parallel()

	got, err := URLForAddress("75111", "Bd*Voltaire")
	if err != nil {
		t.Fatalf("URLForAddress: %v", err)
	}
	q, _ := url.Parse(got)
	if q.Query().Get("libelle_adr_principale_ban") != "ilike.Bd*Voltaire" {
		t.Errorf("libelle_adr_principale_ban = %q", q.Query().Get("libelle_adr_principale_ban"))
	}
}

func TestURLForAddress_MissingInputs(t *testing.T) {
	t.Parallel()

	if _, err := URLForAddress("", "Voltaire"); !errors.Is(err, ErrInsufficientFilter) {
		t.Errorf("URLForAddress(empty insee) = %v", err)
	}
	if _, err := URLForAddress("75111", ""); !errors.Is(err, ErrInsufficientFilter) {
		t.Errorf("URLForAddress(empty pattern) = %v", err)
	}
}

func TestAddressPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, want string
	}{
		{"", ""},
		{"3 Impasse de Mont Louis 75011 Paris", "de Mont Louis"},
		{"106 Boulevard Voltaire 75011 Paris", "Voltaire"},
		{"22 rue Lazare Carnot 92260 Fontenay-aux-Roses", "Lazare Carnot"},
		{"9, rue Aubert", "Aubert"},
		{"30-32, av. André Kervazo", "André Kervazo"},
		{"6 Chem. de Gaillon, 78700 Conflans", "de Gaillon"},
		{"Avenue de la Liberté", "de la Liberté"},
		{"123 alpha beta gamma delta epsilon zeta", "alpha beta gamma"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := AddressPattern(tc.in); got != tc.want {
				t.Errorf("AddressPattern(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseAddress_NumberKept(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in, num string
	}{
		{"3 Impasse de Mont Louis 75011 Paris", "3"},
		{"106 Boulevard Voltaire", "106"},
		{"30-32, av. André Kervazo", "30"},
		{"32B Rue X", "32"},
		{"Avenue de la Liberté", ""},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := ParseAddress(tc.in)
			if got.Number != tc.num {
				t.Errorf("ParseAddress(%q).Number = %q, want %q", tc.in, got.Number, tc.num)
			}
		})
	}
}

func TestAddressPattern_StopsAtPostal(t *testing.T) {
	t.Parallel()

	if got := AddressPattern("75011 Paris"); got != "" {
		t.Errorf("AddressPattern(zip-only) = %q, want empty", got)
	}
}

// IlikePatternFor strips the range-orphan leak that fraddr.Parse leaves
// in the StreetTokens slice. Cases drawn from real BDNB ilike-pattern
// pollution observed in production.
func TestIlikePatternFor_TrimsRangeOrphans(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want string
	}{
		{"Résidence du Parc, 75 bis rue Gambetta et Rue du Général Archinard", "Gambetta et"},
		{"Centre Commercial Charras, 12 à 18 rue Baudin", "Baudin"},
		{"Villa Stevens, 71 à 77 rue Raymond Barbet", "Raymond"},
		{"1 à 39 rue des Mouettes 78960 Voisins", "des"},
		{"Résidence Toscane, 31 ter rue des Ecoles", "des Ecoles"},
		{"82 Rue de la Roquette 75011 Paris", "de la Roquette"},
		{"Avenue de la Liberté", "de la Liberté"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := IlikePatternFor(ParseAddress(tc.in))
			if got != tc.want {
				t.Errorf("IlikePatternFor(Parse(%q)) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestAddressPattern_ResidencePrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in          string
		wantPattern string
		wantNumber  string
	}{
		{
			"Résidence Le Méridien, 32 rue Dareau",
			"Dareau",
			"32",
		},
		{
			"ZAC des Docks, 14 rue des Bateliers",
			"des Bateliers",
			"14",
		},
		{
			"Résidence Tour Sannois, 5 esplanade de l'Europe",
			"de l'Europe",
			"5",
		},
		{
			"Lotissement La Campagne à Paris, 15 rue Irénée Blanc",
			"Irénée Blanc",
			"15",
		},
		{
			"9, rue Aubert",
			"Aubert",
			"9",
		},
		{
			"30-32, av. André Kervazo",
			"André Kervazo",
			"30",
		},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got := ParseAddress(tc.in)
			if got.Pattern() != tc.wantPattern {
				t.Errorf("Pattern() = %q, want %q", got.Pattern(), tc.wantPattern)
			}
			if got.Number != tc.wantNumber {
				t.Errorf("Number = %q, want %q", got.Number, tc.wantNumber)
			}
		})
	}
}
