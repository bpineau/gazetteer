package banx

import (
	"context"
	"errors"
	"strings"
	"time"
)

// ErrNotFound is returned when no result matches the query (or BAN
// returned 0 features).
var ErrNotFound = errors.New("banx: not found")

// Geocoder is the contract for any address → (lat, lon) lookup. The BAN
// implementation lives in this package; tests substitute their own.
type Geocoder interface {
	Geocode(ctx context.Context, q GeocodeQuery) (GeocodeResult, error)
}

// GeocodeQuery is the input. Address is the free-form line; City/Zip are
// optional disambiguation hints concatenated to the query before the
// API call.
type GeocodeQuery struct {
	Address string
	City    string
	Zip     string
}

// String returns the canonical "search query" that we send to BAN. Tests
// rely on this being deterministic for cache-key construction.
func (q GeocodeQuery) String() string {
	parts := make([]string, 0, 3)
	if s := strings.TrimSpace(q.Address); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(q.Zip); s != "" && !containsZip(q.Address, s) {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(q.City); s != "" && !containsToken(q.Address, s) {
		parts = append(parts, s)
	}
	return strings.Join(parts, " ")
}

// GeocodeResult is the BAN-flavoured response. CityCode is the 5-digit
// INSEE code; it is always extracted because every downstream caller
// keyed on the commune (cadastre, DVF, tax, …) needs it.
//
// Lat/Lon alone do not say WHAT they are the position of. Read Precision
// (and Score) before handing them to anything reading at address
// granularity: BAN answers every query it can parse, so a nonexistent
// street comes back as its commune's centre, with coordinates that look
// exactly like an address.
type GeocodeResult struct {
	Lat, Lon float64
	Label    string
	// Score is BAN's own confidence, in (0, 1]. Zero means the payload
	// carried none (a stub, a proxy), never "no confidence".
	Score float64
	// Precision is the granularity the coordinate was matched at,
	// decoded from BAN's `type` property. See the Precision godoc for
	// what each value can be read for; the zero value means unreported.
	Precision Precision
	CityCode  string // INSEE / "citycode" returned by BAN
	PostCode  string
	Source    string // "ban"
	FetchedAt time.Time
}

func containsZip(s, zip string) bool {
	return strings.Contains(s, zip)
}
func containsToken(s, tok string) bool {
	if tok == "" {
		return true
	}
	return strings.Contains(strings.ToLower(s), strings.ToLower(tok))
}

// ZipsShareDepartment reports whether two 5-digit FR postcodes fall in the
// same département. Uses a 2-digit prefix for métropolitain zips and a
// 3-digit prefix for DOM-TOM (97xxx / 98xxx: 971 Guadeloupe, 972 Martinique,
// 973 Guyane, 974 Réunion, 975 Saint-Pierre-et-Miquelon, 976 Mayotte,
// 986/987/988 Polynésie/Wallis/Nouvelle-Calédonie).
//
// Saint-Barthélemy (977) and Saint-Martin (978) are NOT separated by their
// prefix: both kept 971xx postal codes when they were split off Guadeloupe in
// 2007, so 97133 and 97150 are mapped by hand. Without that, a BAN answer in
// Guadeloupe was accepted for a Saint-Barthélemy query — 230 km away, and
// exactly what this guard exists to reject.
//
// Empty inputs are treated as "no anchor → no rejection" (returns true),
// matching the existing semantics in the castorus / bienici /
// meilleursagents enricher pickers.
//
// Anything that is not a well-formed 5-digit zip falls back to equality, so a
// zip that lost its leading zero cannot fold onto the wrong département:
// "1000" (Bourg-en-Bresse, Ain) shares nothing with "10000" (Troyes, Aube),
// where a blind 2-character prefix made them the same.
//
// Exported here so the BAN cache layer and any future geo consumer can
// share a single dept-guard predicate, rather than each enricher
// re-implementing it.
func ZipsShareDepartment(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return true
	}
	if a == b {
		return true
	}
	return deptMatchKey(a) == deptMatchKey(b)
}
