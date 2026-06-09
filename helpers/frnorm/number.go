package frnorm

import (
	"strconv"
	"strings"
	"unicode"
)

// ParseFRFloat parses a French-formatted decimal ("16,4", "1 234,5",
// "1 234,5" with a no-break or narrow no-break space as the thousands
// separator). Every Unicode space is stripped and the comma decimal mark
// is normalised before strconv.ParseFloat. ok is false for an empty or
// unparseable cell.
//
// Special markers ("NA", "s", "n.d."…) vary per producer; callers screen
// them before calling (they all fail to parse, so the ok=false fallback
// is already correct — screening just makes intent explicit).
func ParseFRFloat(s string) (float64, bool) {
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, s)
	s = strings.ReplaceAll(s, ",", ".")
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
