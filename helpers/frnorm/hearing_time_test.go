package frnorm

import "testing"

func TestNormalizeHearingTime(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"french_hhmm", "14h00", "14:00:00"},
		{"french_bare_hour", "14h", "14:00:00"},
		{"french_uppercase", "14H30", "14:30:00"},
		{"french_single_digit_hour", "9h30", "09:30:00"},
		{"colon_hhmm", "14:00", "14:00:00"},
		{"colon_canonical_passthrough", "14:00:00", "14:00:00"},
		{"colon_with_seconds", "09:30:00", "09:30:00"},
		{"strip_parens_paris_annotation", "14h00 (heure de Paris)", "14:00:00"},
		{"strip_parens_french_only", "9h30 (à confirmer)", "09:30:00"},
		{"surrounding_whitespace", "  14h00  ", "14:00:00"},
		{"french_with_spaces", "14 h 00", "14:00:00"},
		{"empty", "", ""},
		{"whitespace_only", "   ", ""},
		{"unparseable_word", "matin", ""},
		{"hour_out_of_range", "25h00", ""},
		{"single_digit_bare_hour", "9h", "09:00:00"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := NormalizeHearingTime(c.in)
			if got != c.want {
				t.Errorf("NormalizeHearingTime(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
