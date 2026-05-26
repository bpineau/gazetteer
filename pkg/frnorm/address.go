package frnorm

import (
	"regexp"
	"strings"
)

// reFrenchZip matches the first 5-digit token in a string with word
// boundaries on both sides. Tight on purpose — French postal codes are
// always exactly 5 digits, never adjacent to other digits or letters.
var reFrenchZip = regexp.MustCompile(`\b(\d{5})\b`)

// reZipTrailingCity matches the canonical "<zip> <city>" tail of a French
// address line, optionally followed by "France" :
//
//	"3 bis Av. du President, 93110 Rosny-sous-Bois, France"
//	"foo, 75011 Paris"
//
// Only matches when the zip+city sit at the END of the string — that's the
// pattern the four scrapers actually receive on listing cards. Multi-line
// addresses or addresses where the zip appears mid-string need
// [ExtractZipFromAddress] plus a source-specific city heuristic.
var reZipTrailingCity = regexp.MustCompile(`\b(\d{5})\s+([^,]+?)(?:,\s*France)?\s*$`)

// ExtractZipFromAddress returns the first 5-digit French postal code found
// in s, with ok=true if a match was made. ok=false when no token matches
// the pattern.
//
// DOM-TOM zips (Guadeloupe / Martinique / Reunion / Mayotte / Saint-Pierre,
// 5-digit codes prefixed by 97x or 98x) are returned as-is — they're real
// French postal codes, just for the overseas territories. Callers that
// only want metropolitan zips must filter the prefix themselves.
//
// The function does NOT validate that the digits form a real zip ; it
// only enforces shape. Pure : no allocations beyond the regex's own
// captures, safe for concurrent use.
func ExtractZipFromAddress(s string) (zip string, ok bool) {
	if s == "" {
		return "", false
	}
	m := reFrenchZip.FindStringSubmatch(s)
	if len(m) < 2 {
		return "", false
	}
	return m[1], true
}

// ExtractZipCity recovers (zip, city) from an address line whose tail
// follows the canonical "<zip> <city>[, France]" shape used by every
// French listing site we scrape :
//
//	"12 rue Foo, 75011 Paris, France" → ("75011", "Paris", true)
//	"4 Imp. Gantz, 69008 Lyon"        → ("69008", "Lyon", true)
//	"foo, 75011 Paris"                → ("75011", "Paris", true)
//	"75001 Paris 1er"                 → ("75001", "Paris 1er", true)
//	"97150 Saint-Martin"              → ("97150", "Saint-Martin", true)  (DOM-TOM)
//	"no zip here"                     → ("", "", false)
//
// The city portion is trimmed of surrounding whitespace and a trailing
// comma. DOM-TOM zips (97x / 98x prefixes) are accepted as ordinary
// French postal codes ; rejecting them would silently drop overseas
// auctions.
//
// This is the *simple* trailing-pattern extractor. Sources that need to
// recover the city when the zip appears mid-address (e.g. lawyer's
// "WIZERNES (62570) ZAC de la Large Patte") layer their own logic on
// top of [ExtractZipFromAddress].
func ExtractZipCity(s string) (zip, city string, ok bool) {
	if s == "" {
		return "", "", false
	}
	m := reZipTrailingCity.FindStringSubmatch(s)
	if len(m) < 3 {
		return "", "", false
	}
	z := m[1]
	c := strings.TrimSpace(m[2])
	c = strings.TrimSuffix(c, ",")
	c = strings.TrimSpace(c)
	// Defensive : the city capture is `[^,]+?` so a zip followed by only
	// whitespace ("75011  ") yields a single-space capture that trims to
	// empty. The caller's contract is "ok=true → city non-empty" ; honour
	// it rather than handing back a half-result.
	if c == "" {
		return "", "", false
	}
	return z, c, true
}
