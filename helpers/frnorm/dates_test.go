package frnorm

import (
	"testing"
	"time"
)

// TestFrenchMonth locks the canonical month-name lookup consolidated from
// the avoventes and lawyer copies. All 12 months × full + abbreviated +
// accented + accent-stripped forms are covered.
func TestFrenchMonth(t *testing.T) {
	cases := []struct {
		in     string
		want   time.Month
		wantOK bool
	}{
		// Full names with accents.
		{"janvier", time.January, true},
		{"février", time.February, true},
		{"mars", time.March, true},
		{"avril", time.April, true},
		{"mai", time.May, true},
		{"juin", time.June, true},
		{"juillet", time.July, true},
		{"août", time.August, true},
		{"septembre", time.September, true},
		{"octobre", time.October, true},
		{"novembre", time.November, true},
		{"décembre", time.December, true},

		// Full names, accent-stripped (legacy avoventes input shape).
		{"fevrier", time.February, true},
		{"aout", time.August, true},
		{"decembre", time.December, true},

		// Case insensitivity.
		{"JANVIER", time.January, true},
		{"Février", time.February, true},
		{"DéCEMBRE", time.December, true},

		// Abbreviations (with and without trailing dot).
		{"janv", time.January, true},
		{"janv.", time.January, true},
		{"févr.", time.February, true},
		{"fev.", time.February, true},
		{"avr.", time.April, true},
		{"juil.", time.July, true},
		{"sept.", time.September, true},
		{"oct.", time.October, true},
		{"nov.", time.November, true},
		{"déc.", time.December, true},
		{"dec", time.December, true},

		// Surrounding whitespace tolerated.
		{"  mars  ", time.March, true},

		// Unknown inputs.
		{"", 0, false},
		{"foobar", 0, false},
		{"jan", 0, false},       // English-only abbreviation, not used in FR
		{"janua", 0, false},     // partial
		{"march", 0, false},     // English
		{"décembres", 0, false}, // plural — not real
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, ok := FrenchMonth(c.in)
			if got != c.want || ok != c.wantOK {
				t.Errorf("FrenchMonth(%q) = (%d, %v), want (%d, %v)",
					c.in, got, ok, c.want, c.wantOK)
			}
		})
	}
}
