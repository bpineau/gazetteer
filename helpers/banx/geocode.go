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
type GeocodeResult struct {
	Lat, Lon  float64
	Label     string
	Score     float64
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

// ZipsShareDepartment reports whether two 5-digit FR postcodes share the
// same département prefix. Uses a 2-digit prefix for métropolitain zips
// and a 3-digit prefix for DOM-TOM (97xxx / 98xxx, where the third digit
// distinguishes territories: 971 Guadeloupe, 972 Martinique, 973 Guyane,
// 974 Réunion, 975 Saint-Pierre-et-Miquelon, 976 Mayotte, 977/978
// Saint-Barthélemy/Saint-Martin, 986/987/988 Polynésie/Wallis/Nouvelle-
// Calédonie).
//
// Empty inputs are treated as "no anchor → no rejection" (returns true),
// matching the existing semantics in the castorus / bienici /
// meilleursagents enricher pickers (memory `zipmatch_enricher_protocol`).
// Malformed inputs shorter than 2 chars fall back to equality.
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
