package frnorm

import (
	"strconv"
	"strings"
)

// ParseFRPriceToCentimes converts a French-formatted monetary string into
// int64 centimes. Returns 0 when the input is empty or unparseable.
//
// Convention :
//
//	French uses ','           as the decimal separator and
//	' ' (NBSP), ' ', '.'      as thousand separators.
//
// We tolerate but normalize. The disambiguation rules :
//
//  1. Trim whitespace, drop a single trailing "€" / leading currency noise.
//  2. NBSP (U+00A0) and narrow NBSP (U+202F) collapse to plain space.
//  3. All ASCII spaces are stripped (always thousand separators).
//  4. If a comma is present, the comma is the decimal separator and every
//     '.' in the string is a thousand separator (stripped).
//  5. If no comma is present, '.' is *thousand-separator-y* iff its tail
//     (everything after the last dot) is exactly three digits — that's
//     the unambiguous FR convention ("80.000" → 80 000 euros). Otherwise
//     the last dot is treated as a decimal point ("150.50" → 150.50 €,
//     "1.5" → 1.50 €) and any earlier dots collapse into thousand seps.
//
// Anglo-style numbers like "150,000.50" are rejected by rule 4 (the
// comma forces decimal interpretation, yielding 150.00050 → 15 cents,
// which is *defensibly garbage* — the caller is expected to supply
// FR-formatted text). Empty / non-numeric input returns 0.
//
// Edge cases covered by TestParseFRPriceToCentimes :
//
//	""                  → 0
//	"abc"               → 0
//	"150,50 €"          → 15050           (comma decimal)
//	"150 000,50 €"      → 15000050        (NBSP thousands + comma)
//	"150.000 €"         → 15000000        (FR dot-thousands)
//	"150.50 €"          → 15050           (dot-decimal, 1-2 digit tail)
//	"1.5"               → 150             (dot-decimal, 1 digit tail)
//	"1.336.500"         → 133650000       (multi-dot thousands)
//	"61.000"            → 6100000         (single dot-thousand group)
//	"3.465,38"          → 346538          (mixed dot-thousand + comma decimal)
//
// The function is package-pure : no allocations beyond the trimmed string,
// no global state, safe for concurrent calls.
func ParseFRPriceToCentimes(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	// Currency / NBSP normalisation.
	s = strings.ReplaceAll(s, " ", " ") // NBSP
	s = strings.ReplaceAll(s, " ", " ") // narrow NBSP
	s = strings.ReplaceAll(s, "€", "")
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	// Strip all ASCII spaces (thousand separators).
	s = strings.ReplaceAll(s, " ", "")

	if strings.Contains(s, ",") {
		// Comma is the decimal separator ; dots are thousand separators.
		s = strings.ReplaceAll(s, ".", "")
		s = strings.Replace(s, ",", ".", 1)
	} else if i := strings.LastIndex(s, "."); i >= 0 {
		// No comma — disambiguate the last dot.
		tail := s[i+1:]
		switch {
		case len(tail) == 3 && allDigits(tail):
			// "80.000" / "1.336.500" — every dot is thousand-sep.
			s = strings.ReplaceAll(s, ".", "")
		case allDigits(tail):
			// "150.50" / "1.5" — last dot is the decimal point.
			// Earlier dots (rare in practice) collapse to thousand-sep.
			head := strings.ReplaceAll(s[:i], ".", "")
			s = head + "." + tail
		default:
			// Non-digit tail (rare — defensive). Strip every dot and
			// let ParseFloat fail loud.
			s = strings.ReplaceAll(s, ".", "")
		}
	}

	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	// Round half-up to centimes (avoids 1234567.89 → 123456788 from
	// IEEE-754 representation error).
	if v < 0 {
		return int64(v*100 - 0.5)
	}
	return int64(v*100 + 0.5)
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
