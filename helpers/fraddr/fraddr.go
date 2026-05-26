// Package fraddr parses free-form French street addresses into a
// structured Parts value (street number with optional bis/ter/quater
// suffix handling, ranges like "30-32" or "46 a 52", and the most
// discriminating street-name tokens with French street-type markers
// stripped).
//
// The parser is tolerant: malformed or partial input always yields a
// best-effort Parts (possibly with empty fields) — callers downstream
// inspect what's populated.
package fraddr

import (
	"regexp"
	"strings"
)

// embeddedHouseNumberRe matches an embedded "<n> <street-type>" anchor
// inside a free-text address. Used by Parse Step 1.5 when the
// comma-segment scan didn't already produce a digit-prefixed canonical.
//
// Constraints:
//   - The number is 1..4 digits ({1,4}) so a 5-digit French postal code
//     like "75011 Paris" is not matched.
//   - Optional bis/ter/quater suffix between number and street-type.
//   - Optional ranges: "12-14", "46 a 52", "5 et 7".
//   - The street-type alternation lists the canonical French abbreviations
//     and full forms; \. is optional after the abbreviated forms; \b
//     anchors the right edge so "ruedeX" doesn't match.
//
// The match is case-insensitive ((?i) prefix). FindStringIndex returns
// the start byte index of the digit run.
//
// NOT supported: postfix "<street-type> ... n°N" or "<street> numero N"
// patterns. In real-world corpora most "numero" instances are "sans
// numero" placeholders and most "n°" instances are cadastral references
// or cross-street/suffix references whose primary house number is
// already captured by Step 1. Extending the regex to handle the rare
// genuinely-fixable shape would mis-anchor the cadastral / cross-street
// cases — treat postfix-numero as a residual non-fixable input.
var embeddedHouseNumberRe = regexp.MustCompile(
	// Range alternatives:
	//   - hyphen range MUST be tight ("30-32") with no surrounding spaces
	//     so "Lot 5 - 12 rue W" is NOT consumed as a "5..12" range; the
	//     leftmost match starts on the inner "12" instead.
	//   - "a" / "et" allow optional spaces ("46 a 52", "5 et 7").
	`(?i)\b\d{1,4}(?:-\d{1,4}|\s+(?:à|et)\s+\d{1,4})?` +
		`(?:\s+(?:bis|ter|quater))?` +
		`\s+(?:rue|avenue|ave|av\.?|bd\.?|boulevard|bld|imp\.?|impasse|` +
		`all\.?|allée|allee|ch\.?|chem\.?|chemin|pl\.?|place|qu\.?|quai|` +
		`sente|sentier|cours|cité|cite|esplanade|faubourg|fbg|` +
		`route|rte|voie|passage|pass|villa|square|sq|parvis|hameau)\b`,
)

// streetTypeTokens are the tokens we treat as "type de voie" markers.
// They are dropped from the parsed output: different sources disagree
// on abbreviations (av. vs Avenue, Chem. vs Chemin, Bd vs Boulevard,
// …), so dropping the type makes downstream queries resilient.
//
// The trailing "." is stripped before lookup, so "av.", "Bd", "Chem." all
// match this set.
var streetTypeTokens = map[string]bool{
	"rue":        true,
	"avenue":     true,
	"av":         true,
	"boulevard":  true,
	"bd":         true,
	"bld":        true,
	"chemin":     true,
	"chem":       true,
	"impasse":    true,
	"imp":        true,
	"place":      true,
	"pl":         true,
	"allee":      true,
	"allée":      true,
	"all":        true,
	"quai":       true,
	"square":     true,
	"sq":         true,
	"route":      true,
	"rte":        true,
	"voie":       true,
	"passage":    true,
	"pass":       true,
	"villa":      true,
	"cours":      true,
	"sentier":    true,
	"chaussee":   true,
	"chaussée":   true,
	"esplanade":  true,
	"parvis":     true,
	"rond-point": true,
	"hameau":     true,
	"lieu-dit":   true,
}

// Parts is the structured output of Parse: street number (if any) + the
// most discriminating words of the street name.
//
// Typical use:
//   - (a) Build a search-engine query or ilike pattern (street name only —
//     dropping the number decouples the caller from APIs that put the
//     street type between the number and the name).
//   - (b) Post-filter API rows by matching the number when the input had
//     one.
type Parts struct {
	// Number is the street number extracted from the input ("3", "82", …)
	// or empty when the input had no leading digit. For a range like
	// "30-32" we keep "30".
	Number string

	// StreetTokens are the cleaned street-name tokens (street-type markers
	// like "rue" / "avenue" stripped, capped at 3).
	StreetTokens []string
}

// Pattern returns the ilike-friendly pattern: the street name tokens joined
// by spaces, with the leading number dropped. The number is filtered
// post-fetch via the caller's own matching logic.
func (p Parts) Pattern() string {
	if len(p.StreetTokens) == 0 {
		return ""
	}
	return strings.Join(p.StreetTokens, " ")
}

// Query returns the full-text query string: the number (when present)
// followed by the street tokens, separated by spaces. Including the number
// boosts the relevance score of rows whose address starts with that number.
func (p Parts) Query() string {
	if len(p.StreetTokens) == 0 && p.Number == "" {
		return ""
	}
	parts := make([]string, 0, 1+len(p.StreetTokens))
	if p.Number != "" {
		parts = append(parts, p.Number)
	}
	parts = append(parts, p.StreetTokens...)
	return strings.Join(parts, " ")
}

// Parse turns a free-text French address into a Parts struct.
//
// Normalisation steps:
//  1. If the address begins with a non-digit (e.g. "Residence Le Meridien,
//     32 rue Dareau"), split on "," and use the first segment whose first
//     non-space character is a digit. This handles the common scrape pattern
//     where a commercial/residence name is prepended before the BAN address.
//     Addresses starting with a digit ("9, rue Aubert", "30-32, av. X") are
//     untouched.
//  2. Strip commas (they break tokenisation and ilike patterns).
//  3. Drop everything from the first 5-digit French postal-code token onward.
//  4. Extract the leading street number (first run of digits; "30-32" → "30",
//     "32B" → "32").
//  5. Drop street-type tokens (see streetTypeTokens).
//  6. Cap remaining tokens to 3 (most discriminating words; beyond 3 the
//     query over-constrains).
//
// Examples:
//
//	"3 Impasse de Mont Louis 75011 Paris"            → {3,  [de Mont Louis]}
//	"106 Boulevard Voltaire 75011 Paris"             → {106, [Voltaire]}
//	"9, rue Aubert"                                  → {9,  [Aubert]}
//	"30-32, av. Andre Kervazo"                       → {30, [Andre Kervazo]}
//	"6 Chem. de Gaillon, 78700 Conflans"             → {6,  [de Gaillon]}
//	"Avenue de la Liberte"                           → {"", [de la Liberte]}
//	"Residence Le Meridien, 32 rue Dareau"           → {32, [Dareau]}
func Parse(addr string) Parts {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return Parts{}
	}

	// Step 1: if the address begins with a non-digit character, split on ","
	// and look for the first segment whose first non-space character is a digit.
	// This skips commercial / residence name prefixes that some scrapers emit.
	// Addresses starting with digits ("9, rue Aubert", "30-32, av. X") bypass
	// this step entirely because addr[0] is already a digit.
	if len(addr) > 0 && (addr[0] < '0' || addr[0] > '9') {
		for seg := range strings.SplitSeq(addr, ",") {
			seg = strings.TrimSpace(seg)
			if len(seg) > 0 && seg[0] >= '0' && seg[0] <= '9' {
				addr = seg
				break
			}
		}
	}

	// Step 1.5 (embedded-anchor fallback): if Step 1 didn't re-anchor
	// (addr still begins with a non-digit), scan the full input for an
	// embedded "<n> <street-type>" pattern and re-anchor on the digit
	// when found. Catches:
	//   "Residence Park Avenue, Adresse postale : 93 bd Rodin"  → "93 bd Rodin"
	//   "ZAC Charas Nord - 6 rue Kleber"                        → "6 rue Kleber"
	//   "A l'angle du 46 a 52 av. de Stalingrad"                → "46 a 52 av. de Stalingrad"
	//
	// The regex caps the leading digit run at 4 chars to prevent matching a
	// 5-digit French postal code ("75011 Paris" stays untouched). If Step 1
	// already produced a digit-prefixed canonical, addr[0] is a digit and
	// this branch is skipped (no double-anchor).
	if len(addr) > 0 && (addr[0] < '0' || addr[0] > '9') {
		if loc := embeddedHouseNumberRe.FindStringIndex(addr); loc != nil {
			addr = strings.TrimSpace(addr[loc[0]:])
		}
	}

	// Step 2: strip commas.
	addr = strings.ReplaceAll(addr, ",", " ")
	fields := strings.Fields(addr)
	if len(fields) == 0 {
		return Parts{}
	}

	// Step 3: drop everything from the first 5-digit postal-code token onward.
	stop := len(fields)
	for i, f := range fields {
		if isFrPostalCode(f) {
			stop = i
			break
		}
	}
	if stop == 0 {
		return Parts{}
	}
	fields = fields[:stop]

	out := Parts{}

	// Step 4: extract leading street number.
	if len(fields) > 0 {
		if extracted := extractLeadingNumber(fields[0]); extracted != "" {
			out.Number = extracted
			fields = fields[1:]
		}
	}

	// Step 5: drop street-type tokens.
	cleaned := make([]string, 0, len(fields))
	for _, f := range fields {
		key := strings.TrimRight(strings.ToLower(f), ".")
		if streetTypeTokens[key] {
			continue
		}
		cleaned = append(cleaned, f)
	}

	// Step 6: cap to 3 tokens.
	if len(cleaned) > 3 {
		cleaned = cleaned[:3]
	}
	out.StreetTokens = cleaned
	return out
}

// extractLeadingNumber returns the first run of digits at the start of s,
// or "" when s does not start with a digit. Stops at the first non-digit
// character.
//
//	"30"     → "30"
//	"30-32"  → "30"
//	"32B"    → "32"
//	"abc"    → ""
func extractLeadingNumber(s string) string {
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	return s[:end]
}

// IsFrPostalCode reports whether s is exactly 5 ASCII digits — heuristic
// used by Parse to chop the commune suffix from an address string and by
// per-enricher zip resolvers to validate user-supplied or geocoded zips
// before falling through to BAN.
func IsFrPostalCode(s string) bool {
	if len(s) != 5 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// isFrPostalCode is the package-private alias kept for the internal Parse
// implementation. New external callers must use the exported IsFrPostalCode.
func isFrPostalCode(s string) bool { return IsFrPostalCode(s) }

// ItoaPositive returns the decimal representation of n as a string.
// Returns "1" for n <= 0. Used by enrichers to format URL query parameters
// (limit/size) without pulling in strconv for small positive constants.
func ItoaPositive(n int) string {
	if n <= 0 {
		return "1"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
