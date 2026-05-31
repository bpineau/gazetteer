// Package ademe is a gazetteer.Source that pulls a logement's DPE
// (Diagnostic de Performance Énergétique) from the ADEME public dataset
// `dpe03existant` (DPE Logements existants depuis 2021-07).
//
// # Strategy
//
// ADEME exposes a public data-fair endpoint at data.ademe.fr. The route
//
//	GET /data-fair/api/v1/datasets/dpe03existant/lines
//
// returns DPE rows scoped by Elasticsearch-style query strings. The
// Source scopes by `code_postal_ban` (indexed) and full-text searches
// the BAN adresse with "<num> <street>" — sorted by relevance score
// then date_etablissement_dpe descending so the most recent DPE for the
// right address wins.
//
// # Match strategy
//
//  1. Resolve the listing's zip — preferring Listing.Zip, falling back
//     to the Options.Geocoder when missing / malformed.
//  2. Query the ADEME endpoint with the zip + the trimmed listing
//     address (number + street tokens).
//  3. Pick the row whose adresse_ban / adresse_brut starts with the
//     listing's number; fall back to the top-_score row when none match.
//  4. Empty result → IsEmpty()==true (Result.SampleSize==0); the
//     framework records Status == StatusOKEmpty.
package ademe

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/bpineau/gazetteer/helpers/fraddr"
	"github.com/bpineau/gazetteer/helpers/frnorm"
)

// ErrEmptyBody is returned by ParseList when the input is empty or
// not parseable as JSON. Treated as a transient error by the Source
// (wrapped as gazetteer.ErrUpstreamUnavailable).
var ErrEmptyBody = errors.New("ademe: empty / unparseable body")

// Row is the trimmed, typed shape of a single ADEME data-fair
// `dpe03existant` line. Only the fields the Source renders into the
// Result are kept; the 100+ other columns are ignored by encoding/json.
//
// SurfaceHabitableLogement is decoded as `*float64` because ADEME
// publishes it as a float (e.g. 38.2). Pointers stay nil when the
// field is absent / null in the JSON.
type Row struct {
	NumeroDPE                string   `json:"numero_dpe"`
	EtiquetteDPE             string   `json:"etiquette_dpe"`
	EtiquetteGES             string   `json:"etiquette_ges"`
	SurfaceHabitableLogement *float64 `json:"surface_habitable_logement"`
	AnneeConstruction        *int     `json:"annee_construction"`
	DateEtablissementDPE     string   `json:"date_etablissement_dpe"`
	DateFinValiditeDPE       string   `json:"date_fin_validite_dpe"`
	AdresseBrut              string   `json:"adresse_brut"`
	AdresseBAN               string   `json:"adresse_ban"`
	TypeBatiment             string   `json:"type_batiment"`
	CodePostalBAN            string   `json:"code_postal_ban"`
	NomCommuneBAN            string   `json:"nom_commune_ban"`
}

// listResponse is the data-fair envelope around the rows. Only
// `results[]` is consumed; `total` and `next` are ignored.
type listResponse struct {
	Total   int   `json:"total"`
	Results []Row `json:"results"`
}

// ParseList decodes a data-fair JSON envelope into the trimmed Row
// slice. Returns ErrEmptyBody on an unparseable body. An envelope with
// `results: []` is returned without error so callers can distinguish
// "no DPE found" from a parser failure.
func ParseList(body []byte) ([]Row, error) {
	if len(body) == 0 {
		return nil, ErrEmptyBody
	}
	var env listResponse
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEmptyBody, err)
	}
	return env.Results, nil
}

// PickBestByNumber narrows rows to those whose adresse_ban (or
// adresse_brut, as fallback) starts with `wantNum` followed by a
// non-digit boundary, then picks one. This is the post-filter applied
// when the full-text query returned several rows on the same street
// and the listing had a known street number — we want "82 RUE", not
// "78 RUE".
//
// When `wantSurface` > 0 and several rows at the same street number
// publish a `surface_habitable_logement` (typical of apartment
// buildings where ADEME holds one DPE per dwelling), the picker
// selects the row whose surface is closest to `wantSurface`. This
// turns "DPE at the listing's address" into "DPE of the dwelling the
// caller actually means". Rows without a surface fall back to the
// first match in document order.
//
// Pass `wantSurface = 0` to skip the surface tie-break entirely (the
// picker then returns the first number-matching row, preserving the
// pre-tie-break behaviour).
//
// Range labels like "80-82" / "80/82" / "80 - 82" are also accepted
// when their right-hand bound matches `wantNum`. Returns (-1, false,
// false) when no row matches; callers then fall back to PickBest.
//
// Street-aware selection (v3): when `wantStreetKey` is non-empty and any
// number-matching row is ALSO on the listing's street (per streetKey),
// the pick is restricted to that street-matching subset — this prevents
// picking "8 Cour des Petites Ecuries" when the caller asked for "8 Rue
// des Petites Ecuries" (both share number 8 and the discriminating name
// tokens, so a number-only filter would otherwise pick the wrong voie).
// Surface stays the tie-break WITHIN the chosen subset (it disambiguates
// dwellings on the SAME street, never across streets). When no
// number-matching row is on the listing's street, the picker falls back
// to the full number-matching set. The third return value reports
// whether the finally-picked row street-matched.
func PickBestByNumber(rows []Row, wantNum, wantStreetKey string, wantSurface float64) (int, bool, bool) {
	wantNum = strings.TrimSpace(wantNum)
	if wantNum == "" {
		return -1, false, false
	}
	var matches []int
	for i, r := range rows {
		if rowStartsWithNumber(r, wantNum) {
			matches = append(matches, i)
		}
	}
	if len(matches) == 0 {
		return -1, false, false
	}
	// Prefer number-matching rows that are also on the right street.
	var onStreet []int
	if wantStreetKey != "" {
		for _, i := range matches {
			if streetMatches(wantStreetKey, rows[i]) {
				onStreet = append(onStreet, i)
			}
		}
	}
	if len(onStreet) > 0 {
		return pickClosestBySurface(rows, onStreet, wantSurface), true, true
	}
	return pickClosestBySurface(rows, matches, wantSurface), true, false
}

// streetTypeAbbrev canonicalises common French voie-type abbreviations
// to their full spelling, so "av des Champs" and "avenue des Champs"
// compare equal. The type word is KEPT (it is the discriminator that
// tells "rue" apart from "cour") — only normalised, never dropped.
var streetTypeAbbrev = map[string]string{
	"av":   "avenue",
	"ave":  "avenue",
	"bd":   "boulevard",
	"bld":  "boulevard",
	"bvd":  "boulevard",
	"blvd": "boulevard",
	"fbg":  "faubourg",
	"fg":   "faubourg",
	"pl":   "place",
	"imp":  "impasse",
	"rte":  "route",
	"sq":   "square",
	"all":  "allee",
	"chem": "chemin",
	"che":  "chemin",
	"pas":  "passage",
	"ste":  "sente",
	"crs":  "cours",
	"qu":   "quai",
	"pte":  "porte",
	"vla":  "villa",
	"cite": "cite",
}

// streetStopwords are French articles / prepositions dropped from a
// street signature so "rue des petites ecuries" and "rue de petites
// ecuries" (rare upstream variants) still compare equal. The type word
// and the proper-name tokens are kept.
var streetStopwords = map[string]bool{
	"de": true, "des": true, "du": true, "d": true,
	"la": true, "le": true, "les": true, "l": true,
	"aux": true, "au": true, "et": true,
}

// streetKey normalises an address into a comparable street signature:
// lowercased + accent-folded, the leading house-number token dropped,
// everything from the first 5-digit postal-code token onward dropped
// (removing "75010 Paris"), French article/preposition stopwords
// dropped, and the leading voie-type word canonicalised via
// streetTypeAbbrev (but kept — it is the discriminator). The remaining
// name tokens are kept in order.
//
//	"8 Rue des Petites Ecuries 75010 Paris"  → "rue petites ecuries"
//	"8 Cour des Petites Ecuries 75010 Paris" → "cour petites ecuries"
//	"12 av. de la République"                → "avenue republique"
//
// Returns "" when no usable street tokens remain (caller treats that as
// "unknown", not a mismatch).
func streetKey(addr string) string {
	folded := frnorm.NormaliseSpace(strings.ToLower(frnorm.StripAccents(addr)))
	if folded == "" {
		return ""
	}
	rawTokens := strings.Fields(folded)
	out := make([]string, 0, len(rawTokens))
	for i, tok := range rawTokens {
		// Drop everything from the first 5-digit postal code onward.
		if fraddr.IsFrPostalCode(tok) {
			break
		}
		// Drop a leading house-number token (with optional bis/ter/
		// letter suffix, e.g. "8", "8b", "80-82").
		if i == 0 && startsWithDigit(tok) {
			continue
		}
		tok = strings.Trim(tok, ".,;:")
		if tok == "" {
			continue
		}
		if streetStopwords[tok] {
			continue
		}
		if canon, ok := streetTypeAbbrev[tok]; ok {
			tok = canon
		}
		out = append(out, tok)
	}
	return strings.Join(out, " ")
}

// startsWithDigit reports whether s begins with an ASCII digit.
func startsWithDigit(s string) bool {
	return s != "" && s[0] >= '0' && s[0] <= '9'
}

// streetTokensEqual compares two street signatures order-insensitively
// (token-set equality on the multiset of tokens). This tolerates the
// occasional token-reordering between adresse_ban and adresse_brut while
// still requiring the SAME type word and SAME name tokens — so "rue
// petites ecuries" never matches "cour petites ecuries".
func streetTokensEqual(a, b string) bool {
	if a == b {
		return true
	}
	if a == "" || b == "" {
		return false
	}
	ta := strings.Fields(a)
	tb := strings.Fields(b)
	if len(ta) != len(tb) {
		return false
	}
	sort.Strings(ta)
	sort.Strings(tb)
	for i := range ta {
		if ta[i] != tb[i] {
			return false
		}
	}
	return true
}

// streetMatches reports whether wantKey (the listing's streetKey) equals
// the streetKey of the row's AdresseBAN OR AdresseBrut. An empty wantKey
// means the query carried no usable street → "unknown", which is treated
// as NOT a match (so it can never upgrade confidence to high) but also
// never used to reject a row (see the caller's fallback).
func streetMatches(wantKey string, r Row) bool {
	if wantKey == "" {
		return false
	}
	if streetTokensEqual(wantKey, streetKey(r.AdresseBAN)) {
		return true
	}
	if streetTokensEqual(wantKey, streetKey(r.AdresseBrut)) {
		return true
	}
	return false
}

// PickBest returns the index of the best row when no number match is
// available. Strategy: prefer rows with a non-empty `etiquette_dpe`
// (the column we actually need), then — when `wantSurface` > 0 —
// the row whose `surface_habitable_logement` is closest to
// `wantSurface`. Rows without a DPE label fall to the very end, rows
// without a surface fall back to document order within the DPE-bearing
// set. The data-fair API already sorts by `_score,
// date_etablissement_dpe desc`, so when no surface anchor is supplied
// the first DPE-bearing row is returned.
//
// Returns (-1, false) on an empty list.
func PickBest(rows []Row, wantSurface float64) (int, bool) {
	if len(rows) == 0 {
		return -1, false
	}
	var withDPE, withoutDPE []int
	for i, r := range rows {
		if r.EtiquetteDPE != "" {
			withDPE = append(withDPE, i)
		} else {
			withoutDPE = append(withoutDPE, i)
		}
	}
	if len(withDPE) > 0 {
		return pickClosestBySurface(rows, withDPE, wantSurface), true
	}
	return pickClosestBySurface(rows, withoutDPE, wantSurface), true
}

// pickClosestBySurface ranks `indices` by |row.SurfaceHabitableLogement
// − wantSurface|, returning the closest match. Rows without a published
// surface sink to the bottom of the ranking. When wantSurface <= 0 or
// no row in the slice publishes a surface, returns indices[0] (the
// pre-existing "first match wins" behaviour).
func pickClosestBySurface(rows []Row, indices []int, wantSurface float64) int {
	if len(indices) == 0 {
		return -1
	}
	if wantSurface <= 0 || len(indices) == 1 {
		return indices[0]
	}
	bestIdx := indices[0]
	bestDelta := math.MaxFloat64
	for _, i := range indices {
		s := rows[i].SurfaceHabitableLogement
		if s == nil || *s <= 0 {
			continue
		}
		d := *s - wantSurface
		if d < 0 {
			d = -d
		}
		if d < bestDelta {
			bestDelta = d
			bestIdx = i
		}
	}
	return bestIdx
}

// rowStartsWithNumber reports whether `r.AdresseBAN` (or AdresseBrut as
// fallback) starts with `num` followed by a non-digit boundary.
//
// Accepted formats:
//
//	"82 Rue de la Roquette"      → matches "82"
//	"82, rue X"                  → matches "82"
//	"82B Rue X"                  → matches "82"  (number 82 + letter B)
//	"80-82 Rue X"                → matches "82"  (range right bound)
//	"80/82 Rue X"                → matches "82"  (slash range)
//	"80 - 82 Rue X"              → matches "82"  (spaced range)
//	"180 Rue X"                  → does NOT match "18" (digit boundary)
func rowStartsWithNumber(r Row, num string) bool {
	if matchAddrNumber(r.AdresseBAN, num) {
		return true
	}
	if matchAddrNumber(r.AdresseBrut, num) {
		return true
	}
	return false
}

// matchAddrNumber checks the leading-number rule on a single address
// string. Exported indirectly via PickBestByNumber; the unexported
// helper makes the unit tests local to this package.
func matchAddrNumber(addr, num string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" || num == "" {
		return false
	}
	if hasNumberPrefix(addr, num) {
		return true
	}
	if rb := rangeRightBound(addr); rb != "" && rb == num {
		return true
	}
	return false
}

// hasNumberPrefix reports whether `s` starts with `num` followed by a
// non-digit (or end-of-string). Returns true for "82 Rue", "82B Rue",
// "82, rue", false for "820 Rue".
func hasNumberPrefix(s, num string) bool {
	if !strings.HasPrefix(s, num) {
		return false
	}
	if len(s) == len(num) {
		return true
	}
	next := s[len(num)]
	return next < '0' || next > '9'
}

// rangeRightBound parses a leading "<lo><sep><hi>" range and returns
// `<hi>` when present, or "" otherwise. Separator is one of `-`, `/`,
// `,`, optionally surrounded by spaces.
//
//	"80-82 Rue X"   → "82"
//	"80 - 82 Rue X" → "82"
//	"80/82 Rue X"   → "82"
//	"82 Rue X"      → ""
func rangeRightBound(s string) string {
	loEnd := 0
	for loEnd < len(s) && s[loEnd] >= '0' && s[loEnd] <= '9' {
		loEnd++
	}
	if loEnd == 0 {
		return ""
	}
	i := loEnd
	for i < len(s) && s[i] == ' ' {
		i++
	}
	if i >= len(s) {
		return ""
	}
	sep := s[i]
	if sep != '-' && sep != '/' && sep != ',' {
		return ""
	}
	i++
	for i < len(s) && s[i] == ' ' {
		i++
	}
	hiStart := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == hiStart {
		return ""
	}
	return s[hiStart:i]
}

// PickConfidence implements the confidence calibration (v3 —
// street-aware):
//
//	high   : street-number matched AND street (type+name) matched AND
//	         etiquette_dpe non-empty — the row is on the right voie at
//	         the right number with a DPE label.
//	medium : a partial match — number matched OR etiquette present, but
//	         NOT all three. Crucially a number-matched, DPE-bearing row
//	         on the WRONG street is medium, never high (the bug fix).
//	low    : nothing matched (caller wraps in an empty/skipped result).
func PickConfidence(matched, numberMatched, streetMatched bool, etiquetteDPE string) string {
	if !matched {
		return ConfidenceLow
	}
	if numberMatched && streetMatched && etiquetteDPE != "" {
		return ConfidenceHigh
	}
	if numberMatched || etiquetteDPE != "" {
		return ConfidenceMedium
	}
	return ConfidenceLow
}

// buildResult renders a Row into the typed Result struct (excluding
// the Confidence / SampleSize / Skipped fields, which are set by the
// caller).
func buildResult(r Row) *Result {
	out := &Result{}

	dpe := DPE{
		EtiquetteDPE:         r.EtiquetteDPE,
		EtiquetteGES:         r.EtiquetteGES,
		NumeroDPE:            r.NumeroDPE,
		DateEtablissementDPE: r.DateEtablissementDPE,
		DateFinValiditeDPE:   r.DateFinValiditeDPE,
	}
	if !dpeEmpty(dpe) {
		out.DPE = &dpe
	}

	log := Logement{
		SurfaceHabitableM2: r.SurfaceHabitableLogement,
		AnneeConstruction:  r.AnneeConstruction,
		TypeBatiment:       r.TypeBatiment,
	}
	if !logementEmpty(log) {
		out.Logement = &log
	}

	adr := Adresse{
		AdresseBrut:   r.AdresseBrut,
		AdresseBAN:    r.AdresseBAN,
		CodePostalBAN: r.CodePostalBAN,
		NomCommuneBAN: r.NomCommuneBAN,
	}
	if !adresseEmpty(adr) {
		out.Adresse = &adr
	}

	return out
}

func dpeEmpty(d DPE) bool {
	return d.EtiquetteDPE == "" && d.EtiquetteGES == "" &&
		d.NumeroDPE == "" && d.DateEtablissementDPE == "" &&
		d.DateFinValiditeDPE == ""
}

func logementEmpty(l Logement) bool {
	return l.SurfaceHabitableM2 == nil && l.AnneeConstruction == nil &&
		l.TypeBatiment == ""
}

func adresseEmpty(a Adresse) bool {
	return a.AdresseBrut == "" && a.AdresseBAN == "" &&
		a.CodePostalBAN == "" && a.NomCommuneBAN == ""
}

// ParseAddress turns a free-text address into the structured AddressParts
// used to build the ADEME query.
//
// This is a type alias for fraddr.Parts, re-exported for ergonomics so
// callers don't need to import fraddr directly.
type AddressParts = fraddr.Parts

// ParseAddress turns a free-text address into an AddressParts struct.
// See fraddr.Parse for the full normalisation rules.
func ParseAddress(addr string) AddressParts {
	return fraddr.Parse(addr)
}
