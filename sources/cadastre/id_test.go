package cadastre

import (
	"strings"
	"testing"
)

func TestParcelID_PaddingMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		insee   string
		prefixe string
		section string
		numero  string
		want    string
	}{
		{
			name:    "paris_arrondissement",
			insee:   "75104",
			prefixe: "000",
			section: "AE",
			numero:  "0003",
			want:    "75104000AE0003",
		},
		{
			name:    "small_commune_two_char_section",
			insee:   "78638",
			prefixe: "000",
			section: "0B",
			numero:  "0698",
			want:    "786380000B0698",
		},
		{
			name:    "section_left_padded_from_1char",
			insee:   "78005",
			prefixe: "000",
			section: "A",
			numero:  "0285",
			want:    "780050000A0285",
		},
		{
			name:    "numero_padded_from_short",
			insee:   "78005",
			prefixe: "000",
			section: "BB",
			numero:  "5",
			want:    "78005000BB0005",
		},
		{
			name:    "non_default_prefix",
			insee:   "13208",
			prefixe: "050",
			section: "AB",
			numero:  "0123",
			want:    "13208050AB0123",
		},
		{
			name:    "empty_insee_yields_empty",
			insee:   "",
			prefixe: "000",
			section: "AB",
			numero:  "0001",
			want:    "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ParcelID(tc.insee, tc.prefixe, tc.section, tc.numero)
			if got != tc.want {
				t.Errorf("ParcelID(%q,%q,%q,%q) = %q, want %q",
					tc.insee, tc.prefixe, tc.section, tc.numero, got, tc.want)
			}
			// Sanity-check id length when not empty.
			if tc.want != "" && len(got) != 14 {
				t.Errorf("ParcelID length = %d, want 14 (got %q)", len(got), got)
			}
		})
	}
}

func TestMapURL_Happy(t *testing.T) {
	t.Parallel()

	got := MapURL("75104000AE0003")
	want := "https://cadastre.data.gouv.fr/map?style=ortho&parcelleId=75104000AE0003"
	if got != want {
		t.Errorf("MapURL = %q\nwant %q", got, want)
	}
}

func TestMapURL_EmptyID(t *testing.T) {
	t.Parallel()

	if got := MapURL(""); got != "" {
		t.Errorf("MapURL(\"\") = %q, want empty", got)
	}
}

func TestParcelID_Numero4DigitNoTruncation(t *testing.T) {
	t.Parallel()

	// "1234" is already 4 chars — must NOT be padded.
	got := ParcelID("75104", "000", "AE", "1234")
	if !strings.HasSuffix(got, "1234") {
		t.Errorf("ParcelID suffix = ...%s, want ...1234 (got %q)", got[len(got)-4:], got)
	}
}
