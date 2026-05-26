package frnorm

import (
	"strings"
	"time"
)

// frenchMonthLookup is the canonical French-month-name → time.Month table
// shared by every source. Keys are LOWERCASE. Both the accented and the
// accent-stripped forms are listed so callers don't need to fold accents
// before lookup.
//
// Abbreviations seen in the wild on lawyer / avoventes / vench cabinets :
//
//	"janv." / "janv"             → January
//	"fevr." / "fevr" / "fev."    → February
//	"avr." / "avr"               → April
//	"juil."                      → July
//	"sept." / "sept"             → September
//	"oct." / "nov." / "dec."     → October / November / December
//
// All entries are written without the trailing dot ; FrenchMonth strips
// dots and surrounding whitespace before lookup so callers don't have to.
var frenchMonthLookup = map[string]time.Month{
	// Full names (accented).
	"janvier":   time.January,
	"février":   time.February,
	"mars":      time.March,
	"avril":     time.April,
	"mai":       time.May,
	"juin":      time.June,
	"juillet":   time.July,
	"août":      time.August,
	"septembre": time.September,
	"octobre":   time.October,
	"novembre":  time.November,
	"décembre":  time.December,

	// Full names (accent-stripped form, often emitted by upstream after
	// their own normalisation).
	"fevrier":  time.February,
	"aout":     time.August,
	"decembre": time.December,

	// Abbreviated forms.
	"janv":  time.January,
	"févr":  time.February,
	"fev":   time.February,
	"févr.": time.February, // tolerated; FrenchMonth strips the dot first
	"fév":   time.February,
	"avr":   time.April,
	"juil":  time.July,
	"sept":  time.September,
	"oct":   time.October,
	"nov":   time.November,
	"déc":   time.December,
	"dec":   time.December,
}

// FrenchMonth returns the time.Month for a French month name.
// Case-insensitive ; tolerates a trailing dot ("janv.") and surrounding
// whitespace. Accepts both the accented and accent-stripped forms
// (e.g. "fevrier" and "fevrier" both return time.February).
//
// Returns (0, false) for unknown inputs. The 0 month value is invalid in
// time.Date so callers can use the boolean directly OR safely pass 0
// through ; we return both for ergonomics.
func FrenchMonth(s string) (time.Month, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	// Strip a trailing dot (accept "janv." as well as "janv").
	s = strings.TrimRight(s, ".")
	s = strings.ToLower(s)
	if m, ok := frenchMonthLookup[s]; ok {
		return m, true
	}
	return 0, false
}
