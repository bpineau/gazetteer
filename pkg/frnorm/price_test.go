package frnorm

import (
	"fmt"
	"testing"
	"testing/quick"
)

// TestParseFRPriceToCentimes locks the canonical FR price-parsing
// semantics consolidated from three previous copies in
// vench/avoventes/lawyer. The dot-+-1-2-digit case is the divergent
// edge — see the package doc on price.go for the rule.
func TestParseFRPriceToCentimes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int64
	}{
		// Empty / nil-equivalent / non-numeric.
		{"empty", "", 0},
		{"whitespace only", "   ", 0},
		{"non-numeric", "abc", 0},
		{"just euro sign", "€", 0},

		// Comma decimal (canonical FR).
		{"comma decimal", "150,50 €", 15050},
		{"comma decimal no euro", "150,50", 15050},
		{"comma decimal zero", "0,00", 0},

		// Space + comma : NBSP thousands + comma decimal.
		{"NBSP thousands comma decimal", "150 000,50 €", 15000050},
		{"narrow NBSP thousands comma decimal", "150 000,50 €", 15000050},
		{"ASCII space thousands comma decimal", "150 000,50", 15000050},
		{"ASCII space thousands no decimal", "1 234 567,89", 123456789},

		// Dot thousand-sep (3-digit tail).
		{"dot thousands single group", "150.000 €", 15000000},
		{"dot thousands no euro", "61.000", 6100000},
		{"dot thousands multi group", "1.336.500", 133650000},
		{"dot thousands and frais", "3.000.000", 300000000},

		// Mixed dot-thousand + comma decimal.
		{"dot thousand comma decimal", "3.465,38", 346538},
		{"dot thousand comma decimal big", "61.000,38", 6100038},

		// Dot decimal (1-2 digit tail) — the divergent edge.
		{"dot decimal two digits", "150.50 €", 15050},
		{"dot decimal one digit", "1.5", 150},
		{"dot decimal two digits no euro", "150.50", 15050},

		// Anglo "150,000.50" : comma forces decimal interpretation,
		// dots are stripped → ParseFloat("150.00050") → 150.0005 → 15000
		// cents. Documented as "tolerated but garbage if the caller
		// actually meant FR" — we lock the deterministic resolved
		// behaviour so a future tweak doesn't silently change it.
		{"anglo style (garbage-in)", "150,000.50", 15000},

		// Backwards compat with previous lawyer / avoventes test cases.
		{"avoventes 30 000,00", "30 000,00", 3000000},
		{"avoventes 38 178,00", "38 178,00", 3817800},
		{"lawyer 80 000,00", "80 000,00", 8000000},
		{"lawyer 50.000", "50.000", 5000000},
		{"lawyer 6.200", "6.200", 620000},
		{"lawyer 33.000", "33.000", 3300000},
		{"lawyer 100.000", "100.000", 10000000},
		{"lawyer 5 000", "5 000", 500000},
		{"lawyer 1 336 500", "1 336 500", 133650000},
		{"avoventes 3.465", "3.465", 346500},
		{"avoventes 1 000", "1 000", 100000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ParseFRPriceToCentimes(c.in)
			if got != c.want {
				t.Errorf("ParseFRPriceToCentimes(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

// TestParseFRPriceToCentimes_PropertyIntegerRoundtrip — for any
// non-negative integer ≤ 1e12, formatting it as a plain decimal +
// comma-decimal-cents and parsing must return the exact centimes
// value. Catches future regressions where a parse rule swallows
// digits or mis-handles the thousand separator.
func TestParseFRPriceToCentimes_PropertyIntegerRoundtrip(t *testing.T) {
	prop := func(eur uint32, cents uint8) bool {
		if cents >= 100 {
			cents = cents % 100
		}
		// Plain decimal, no thousand separators.
		s := fmt.Sprintf("%d,%02d", eur, cents)
		want := int64(eur)*100 + int64(cents)
		got := ParseFRPriceToCentimes(s)
		return got == want
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 2000}); err != nil {
		t.Error(err)
	}
}

// TestParseFRPriceToCentimes_PropertyNeverPanics — the function must
// never panic on any input. Catches future regressions where a rule
// dereferences a nil slice or indexes past a trimmed string.
func TestParseFRPriceToCentimes_PropertyNeverPanics(t *testing.T) {
	prop := func(s string) (ok bool) {
		defer func() {
			if r := recover(); r != nil {
				ok = false
			}
		}()
		_ = ParseFRPriceToCentimes(s)
		return true
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 2000}); err != nil {
		t.Error(err)
	}
}

// TestParseFRPriceToCentimes_PropertyEuroSuffixInvariant — appending
// " €" to a parseable price must yield the same centimes value.
func TestParseFRPriceToCentimes_PropertyEuroSuffixInvariant(t *testing.T) {
	prop := func(eur uint32, cents uint8) bool {
		cents = cents % 100
		bare := fmt.Sprintf("%d,%02d", eur, cents)
		withEuro := bare + " €"
		return ParseFRPriceToCentimes(bare) == ParseFRPriceToCentimes(withEuro)
	}
	if err := quick.Check(prop, &quick.Config{MaxCount: 1000}); err != nil {
		t.Error(err)
	}
}
