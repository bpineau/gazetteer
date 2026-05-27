package frnorm

import "strings"

// NormaliseSpace collapses any run of whitespace — including ASCII space,
// tab, newline, carriage-return and Unicode non-breaking space (U+00A0) —
// into a single ASCII space, then trims the result.
//
// This is the canonical implementation, consolidated from identical copies in
// a sibling module and a sibling module Vench does not use
// this function (its whitespace handling is embedded in its slug helper).
//
// The function is pure, allocation-bounded and safe for concurrent use.
func NormaliseSpace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	wasSpace := true // treat start as space to skip leading whitespace
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ' ' {
			if !wasSpace {
				b.WriteByte(' ')
			}
			wasSpace = true
			continue
		}
		b.WriteRune(r)
		wasSpace = false
	}
	return strings.TrimRight(b.String(), " ")
}
