package frnorm

import (
	"math"
	"testing"
)

func TestParseFRFloat(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"16,4", 16.4, true},
		{"1 234,5", 1234.5, true},
		{"1 234,5", 1234.5, true},      // no-break space (INSEE)
		{"1 234,5", 1234.5, true},      // narrow no-break space (INSEE)
		{"  9,75769\t", 9.75769, true}, // surrounding whitespace
		{"-3,2", -3.2, true},
		{"12.5", 12.5, true}, // already dot-decimal
		{"42", 42, true},
		{"", 0, false},
		{"   ", 0, false},
		{"NA", 0, false},
		{"n.d.", 0, false},
		{"1,2,3", 0, false}, // double comma → "1.2.3"
	}
	for _, c := range cases {
		got, ok := ParseFRFloat(c.in)
		if ok != c.ok || math.Abs(got-c.want) > 1e-9 {
			t.Errorf("ParseFRFloat(%q) = (%v, %v), want (%v, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}
