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
			name:    "corsica_two_letter_department",
			insee:   "2A004",
			prefixe: "000",
			section: "AB",
			numero:  "0123",
			want:    "2A004000AB0123",
		},
		{
			name:    "empty_insee_yields_empty",
			insee:   "",
			prefixe: "000",
			section: "AB",
			numero:  "0001",
			want:    "",
		},
		{
			// A code that lost its leading zero to a spreadsheet or a
			// JSON number. It used to be padded on the RIGHT, turning
			// Ambérieux (01053, Ain) into 10530, a real commune of the
			// Aube 350 km away, inside a 14-character id that looks
			// perfect and a map deeplink onto a stranger's parcel.
			name:    "leading_zero_eaten_is_refused_not_guessed",
			insee:   "1053",
			prefixe: "000",
			section: "AE",
			numero:  "0003",
			want:    "",
		},
		{
			name:    "over_long_insee_is_refused",
			insee:   "751040",
			prefixe: "000",
			section: "AE",
			numero:  "0003",
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

// TestMakeParcel_INSEEMatchesTheID pins the invariant Parcel.INSEE's
// godoc always claimed and the code did not honour: the commune code is
// the one the id anchors on.
//
// API Carto answers a Paris parcel with code_insee = 75056, the PARENT
// commune, and idu = 75104000AE0003, the ARRONDISSEMENT. Parcel.ID took
// the idu and Parcel.INSEE took code_insee, so the two disagreed on
// every Paris, Lyon and Marseille parcel, and the field's own doc named
// the value it did not carry.
func TestMakeParcel_INSEEMatchesTheID(t *testing.T) {
	t.Parallel()

	p := MakeParcel("75104000AE0003", "75056", "000", "AE", "0003", 15168)
	if p.INSEE != "75104" {
		t.Errorf("INSEE = %q, want 75104 (the code the id embeds), not the parent 75056", p.INSEE)
	}
	if p.ID[:5] != p.INSEE {
		t.Errorf("ID = %q and INSEE = %q disagree", p.ID, p.INSEE)
	}

	// Outside Paris / Lyon / Marseille the two were already the same.
	q := MakeParcel("", "78638", "000", "0B", "0698", 500)
	if q.INSEE != "78638" || q.ID != "786380000B0698" {
		t.Errorf("MakeParcel(small commune) = {ID %q, INSEE %q}, want {786380000B0698, 78638}", q.ID, q.INSEE)
	}
}
