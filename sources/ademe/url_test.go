package ademe

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestURLForAddress_Happy(t *testing.T) {
	t.Parallel()

	got, err := URLForAddress("", "75011", "82 Roquette")
	if err != nil {
		t.Fatalf("URLForAddress: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	if u.Path != "/data-fair/api/v1/datasets/dpe03existant/lines" {
		t.Errorf("path = %q", u.Path)
	}
	q := u.Query()
	// data-fair filters by the `_in` variant; the bare code_postal_ban param
	// is silently ignored by the API (returns the whole dataset).
	if q.Get("code_postal_ban_in") != "75011" {
		t.Errorf("code_postal_ban_in = %q, want 75011", q.Get("code_postal_ban_in"))
	}
	if q.Get("code_postal_ban") != "" {
		t.Errorf("bare code_postal_ban must not be set (it is inert), got %q", q.Get("code_postal_ban"))
	}
	if q.Get("qs") != "" {
		t.Errorf("qs should be empty (Elasticsearch syntax rejected by upstream), got %q", q.Get("qs"))
	}
	if q.Get("q") != "82 Roquette" {
		t.Errorf("q = %q", q.Get("q"))
	}
	if q.Get("q_fields") != "adresse_ban" {
		t.Errorf("q_fields = %q", q.Get("q_fields"))
	}
	if q.Get("size") != "10" {
		t.Errorf("size = %q", q.Get("size"))
	}
	if q.Get("sort") != "-_score,-date_etablissement_dpe" {
		t.Errorf("sort = %q", q.Get("sort"))
	}
	if !strings.Contains(q.Get("select"), "etiquette_dpe") {
		t.Errorf("select missing etiquette_dpe: %q", q.Get("select"))
	}
}

func TestURLForAddress_CustomBase(t *testing.T) {
	t.Parallel()

	got, err := URLForAddress("http://stub.local/x", "75011", "Roquette")
	if err != nil {
		t.Fatalf("URLForAddress: %v", err)
	}
	if !strings.HasPrefix(got, "http://stub.local/x?") {
		t.Errorf("URL = %q, want prefix http://stub.local/x?", got)
	}
}

func TestURLForAddress_MissingInputs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		zip, query string
	}{
		{"", "Roquette"},
		{"75011", ""},
		{"  ", "  "},
	}
	for _, tc := range cases {
		_, err := URLForAddress("", tc.zip, tc.query)
		if !errors.Is(err, ErrInsufficientFilter) {
			t.Errorf("URLForAddress(%q,%q) = %v, want ErrInsufficientFilter",
				tc.zip, tc.query, err)
		}
	}
}

func TestParseAddress(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in     string
		num    string
		tokens []string
	}{
		{"", "", nil},
		{"82 Rue de la Roquette 75011 Paris", "82", []string{"de", "la", "Roquette"}},
		{"3 Impasse de Mont Louis 75011 Paris", "3", []string{"de", "Mont", "Louis"}},
		{"22 rue Lazare Carnot 92260 Fontenay-aux-Roses", "22", []string{"Lazare", "Carnot"}},
		{"9, rue Aubert", "9", []string{"Aubert"}},
		{"30-32, av. André Kervazo", "30", []string{"André", "Kervazo"}},
		{"6 Chem. de Gaillon, 78700 Conflans", "6", []string{"de", "Gaillon"}},
		{"Avenue de la Liberté", "", []string{"de", "la", "Liberté"}},
		{"123 alpha beta gamma delta epsilon zeta", "123", []string{"alpha", "beta", "gamma"}},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := ParseAddress(tc.in)
			if got.Number != tc.num {
				t.Errorf("ParseAddress(%q).Number = %q, want %q", tc.in, got.Number, tc.num)
			}
			if !equalStringSlice(got.StreetTokens, tc.tokens) {
				t.Errorf("ParseAddress(%q).StreetTokens = %v, want %v",
					tc.in, got.StreetTokens, tc.tokens)
			}
		})
	}
}

func TestParseAddress_StopsAtPostal(t *testing.T) {
	t.Parallel()

	got := ParseAddress("75011 Paris")
	if got.Number != "" || len(got.StreetTokens) != 0 {
		t.Errorf("ParseAddress(zip-only) = %+v, want empty", got)
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
