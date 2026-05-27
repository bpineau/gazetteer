package ademe

import (
	"errors"
	"net/url"
	"strings"

	"github.com/bpineau/gazetteer/helpers/fraddr"
)

// DefaultBaseURL is the ADEME data-fair endpoint root for the
// `dpe03existant` dataset (DPE Logements existants depuis 2021-07).
const DefaultBaseURL = "https://data.ademe.fr/data-fair/api/v1/datasets/dpe03existant/lines"

// DefaultLimit is the data-fair `size` applied to each query. At most a
// handful of DPE rows per (zip, address) combination are expected; cap
// at 10 to give the post-filter (PickBestByNumber) elbow room — the
// same street may have several DPE rows for different apartment
// numbers.
const DefaultLimit = 10

// SelectFields is the comma-separated list of columns the Source
// requests via data-fair's `select=`. Trims the response from 100+
// columns down to the dozen the parser actually consumes.
//
// Order matches parser.Row groupings (DPE, building, address) for
// readability.
const SelectFields = "" +
	// DPE
	"numero_dpe," +
	"etiquette_dpe," +
	"etiquette_ges," +
	"date_etablissement_dpe," +
	"date_fin_validite_dpe," +
	// Logement
	"surface_habitable_logement," +
	"annee_construction," +
	"type_batiment," +
	// Address
	"adresse_brut," +
	"adresse_ban," +
	"code_postal_ban," +
	"nom_commune_ban"

// SortOrder is the data-fair `sort=` applied: highest full-text score
// first, then most recent DPE establishment date.
const SortOrder = "-_score,-date_etablissement_dpe"

// QFields is the data-fair `q_fields=` applied: full-text search is
// restricted to `adresse_ban` (the BAN-normalised address) so arbitrary
// `adresse_brut` strings (free-form, diagnostiqueur-typed, noisy) are
// not matched.
const QFields = "adresse_ban"

// MatchStrategy enumerates the supported lookup modes. Recorded in any
// downstream payload's method.params for reproducibility (a downstream consumer
// adapter consumes this).
type MatchStrategy string

const (
	// MatchByZipFulltext is the only mode in v1: scope by zip
	// (`code_postal_ban`) + full-text search the address.
	MatchByZipFulltext MatchStrategy = "zip_fulltext"
)

// ErrInsufficientFilter is returned by URLForAddress when its inputs
// cannot produce a query the ADEME API will accept (no zip OR no
// query string). The Source wraps this as gazetteer.ErrInsufficientInputs.
var ErrInsufficientFilter = errors.New("ademe: insufficient filter inputs")

// URLForAddress builds the ADEME data-fair URL filtering by zip
// (direct `code_postal_ban=<zip>` field-equality param) + a full-text
// query on the BAN adresse field (`q=<query>&q_fields=adresse_ban`).
//
// The Elasticsearch-style `qs=` operator is intentionally avoided —
// ADEME's data-fair layer rejects it with HTTP 403 (verified
// empirically 2026-05-12; equivalent results via field-equality).
//
// Both `zip` and `query` are required.
func URLForAddress(baseURL, zip, query string) (string, error) {
	zip = strings.TrimSpace(zip)
	query = strings.TrimSpace(query)
	if zip == "" || query == "" {
		return "", ErrInsufficientFilter
	}
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	q := url.Values{}
	q.Set("code_postal_ban", zip)
	q.Set("q", query)
	q.Set("q_fields", QFields)
	q.Set("size", fraddr.ItoaPositive(DefaultLimit))
	q.Set("sort", SortOrder)
	q.Set("select", SelectFields)
	return baseURL + "?" + q.Encode(), nil
}
