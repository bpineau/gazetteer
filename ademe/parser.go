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
	"strings"

	"github.com/bpineau/gazetteer/pkg/fraddr"
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

// PickBestByNumber tries to find the index of the row whose adresse_ban
// (or adresse_brut, as fallback) starts with `wantNum` followed by a
// non-digit. This is the post-filter applied when the full-text query
// returned several rows on the same street and the listing had a known
// street number — we want "82 RUE", not "78 RUE".
//
// Range labels like "80-82" / "80/82" / "80 - 82" are also accepted
// when their right-hand bound matches `wantNum`. Returns (-1, false)
// when no row matches; callers then fall back to PickBest.
func PickBestByNumber(rows []Row, wantNum string) (int, bool) {
	wantNum = strings.TrimSpace(wantNum)
	if wantNum == "" {
		return -1, false
	}
	for i, r := range rows {
		if rowStartsWithNumber(r, wantNum) {
			return i, true
		}
	}
	return -1, false
}

// PickBest returns the index of the best row when no number match is
// available. Strategy: prefer rows with a non-empty `etiquette_dpe`
// (the column we actually need), then the most recent DPE
// (`date_etablissement_dpe` desc). The data-fair API already sorts by
// `_score, date_etablissement_dpe desc`, so without further filtering
// rows[0] is typically returned.
//
// Returns (-1, false) on an empty list.
func PickBest(rows []Row) (int, bool) {
	if len(rows) == 0 {
		return -1, false
	}
	for i, r := range rows {
		if r.EtiquetteDPE != "" {
			return i, true
		}
	}
	return 0, true
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

// PickConfidence implements the confidence calibration:
//
//	high   : row matched by exact street-number AND etiquette_dpe non-empty.
//	medium : either number matched OR etiquette_dpe non-empty, not both.
//	low    : neither (caller wraps in an empty/skipped result).
func PickConfidence(matched bool, numberMatched bool, etiquetteDPE string) string {
	if !matched {
		return ConfidenceLow
	}
	if numberMatched && etiquetteDPE != "" {
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
