package bdnb

import (
	"errors"
	"net/url"
	"strings"

	"github.com/bpineau/gazetteer/helpers/fraddr"
)

// BaseURL is the BDNB PostgREST endpoint root. Variable (not const)
// so tests can swap it with httptest.NewServer.URL — same pattern as
// bienici / locservice.
var BaseURL = "https://api.bdnb.io/v1/bdnb/donnees/batiment_groupe_complet"

// DefaultLimit is the PostgREST `limit` applied. At most a handful of
// batiment-groupes per filter are expected; 5 is plenty.
const DefaultLimit = 5

// SelectFields is the canonical comma-separated list of columns the
// Source requests via PostgREST's `select=`. Trims the response from
// 100+ columns down to ~30 fields the parser actually consumes —
// keeping payload size predictable + saving bandwidth.
//
// Order matches Row groupings (identity, building, DPE, risks,
// reliability) for readability.
const SelectFields = "" +
	// identity
	"batiment_groupe_id," +
	"cle_interop_adr_principale_ban," +
	"libelle_adr_principale_ban," +
	"code_commune_insee," +
	"code_iris," +
	"code_epci_insee," +
	"code_departement_insee," +
	"l_parcelle_id," +
	// building
	"annee_construction," +
	"annee_construction_dpe," +
	"nb_log," +
	"nb_niveau," +
	"surface_emprise_sol," +
	"hauteur_mean," +
	"usage_principal_bdnb_open," +
	"type_batiment_dpe," +
	// DPE
	"classe_bilan_dpe," +
	"classe_conso_energie_arrete_2012," +
	"conso_3_usages_ep_m2_arrete_2012," +
	"conso_5_usages_ep_m2," +
	"emission_ges_3_usages_ep_m2_arrete_2012," +
	"emission_ges_5_usages_m2," +
	"type_energie_chauffage," +
	"type_generateur_chauffage," +
	"type_isolation_mur_exterieur," +
	"type_vitrage," +
	"nb_classe_bilan_dpe_a," +
	"nb_classe_bilan_dpe_b," +
	"nb_classe_bilan_dpe_c," +
	"nb_classe_bilan_dpe_d," +
	"nb_classe_bilan_dpe_e," +
	"nb_classe_bilan_dpe_f," +
	"nb_classe_bilan_dpe_g," +
	"nb_classe_conso_energie_arrete_2012_a," +
	"nb_classe_conso_energie_arrete_2012_b," +
	"nb_classe_conso_energie_arrete_2012_c," +
	"nb_classe_conso_energie_arrete_2012_d," +
	"nb_classe_conso_energie_arrete_2012_e," +
	"nb_classe_conso_energie_arrete_2012_f," +
	"nb_classe_conso_energie_arrete_2012_g," +
	"nb_classe_conso_energie_arrete_2012_nc," +
	// risks / urbanism
	"distance_monument_historique," +
	"nom_batiment_historique_plus_proche," +
	"perimetre_bat_historique," +
	"contrainte_urbanisme_ac1," +
	"zone_plu_bati_patrimonial," +
	"quartier_prioritaire," +
	"nom_qp," +
	"nom_quartier_qpv," +
	"valeur_fonciere_m2_residentiel_rel_commune," +
	// reliability
	"fiabilite_cr_adr_niv_1," +
	"fiabilite_cr_adr_niv_2"

// MatchStrategy enumerates the supported lookup modes. The Source
// records the chosen strategy in the Evidence sidecar so downstream
// callers (e.g. encheridor's adapter) can flag medium-confidence
// results.
type MatchStrategy string

const (
	// MatchByBANID is the precise mode: filter on the building's
	// `cle_interop_adr_principale_ban` (BAN id principal). One result
	// per building expected.
	MatchByBANID MatchStrategy = "ban_id"

	// MatchByAddressILike is the fallback mode: scope by INSEE +
	// case-insensitive ILIKE on `libelle_adr_principale_ban`. May
	// return several rows; the parser picks the most complete one.
	MatchByAddressILike MatchStrategy = "address_ilike"
)

// ErrInsufficientFilter is returned by URLForBANID / URLForAddress
// when their inputs cannot produce a query the BDNB API will accept.
// The Source wraps this as gazetteer.ErrInsufficientInputs.
var ErrInsufficientFilter = errors.New("bdnb: insufficient filter inputs")

// URLForBANID builds the URL filtering by INSEE (indexed) +
// `cle_interop_adr_principale_ban` exact match. INSEE is required —
// without it the PostgREST query times out (we observed HTTP 500
// 57014 on the live API).
func URLForBANID(insee, banID string) (string, error) {
	insee = strings.TrimSpace(insee)
	banID = strings.TrimSpace(banID)
	if insee == "" || banID == "" {
		return "", ErrInsufficientFilter
	}
	q := url.Values{}
	q.Set("code_commune_insee", "eq."+insee)
	q.Set("cle_interop_adr_principale_ban", "eq."+banID)
	q.Set("select", SelectFields)
	q.Set("limit", fraddr.ItoaPositive(DefaultLimit))
	return BaseURL + "?" + q.Encode(), nil
}

// URLForAddress builds the URL filtering by INSEE (indexed) + a
// case-insensitive ILIKE pattern on `libelle_adr_principale_ban`. The
// `pattern` is wrapped with `*` wildcards on both sides if it does not
// already contain one — PostgREST converts `*` to `%`.
//
// INSEE is required for the same reason as URLForBANID.
func URLForAddress(insee, pattern string) (string, error) {
	insee = strings.TrimSpace(insee)
	pattern = strings.TrimSpace(pattern)
	if insee == "" || pattern == "" {
		return "", ErrInsufficientFilter
	}
	if !strings.Contains(pattern, "*") {
		pattern = "*" + pattern + "*"
	}
	q := url.Values{}
	q.Set("code_commune_insee", "eq."+insee)
	q.Set("libelle_adr_principale_ban", "ilike."+pattern)
	q.Set("select", SelectFields)
	q.Set("limit", fraddr.ItoaPositive(DefaultLimit))
	return BaseURL + "?" + q.Encode(), nil
}

// AddressParts is the structured output of ParseAddress: street number
// (if any) + the most discriminating words of the street name. Used by
// the Source to (a) build the ilike pattern (street name only —
// dropping the number lets the ilike match BDNB rows that have a
// different street-type label) and (b) post-filter the BDNB rows by
// matching number when the listing had one.
//
// This is a type alias for fraddr.Parts, re-exported for ergonomics.
type AddressParts = fraddr.Parts

// ParseAddress turns a free-text address into an AddressParts struct.
// See AddressPattern for the back-compat front-end and the spec list
// of normalisation steps. Examples:
//
//	"3 Impasse de Mont Louis 75011 Paris"     → {3, [de Mont Louis]}
//	"106 Boulevard Voltaire 75011 Paris"      → {106, [Voltaire]}
//	"9, rue Aubert"                            → {9, [Aubert]}
//	"30-32, av. Andre Kervazo"                 → {30, [Andre Kervazo]}
//	"6 Chem. de Gaillon, 78700 Conflans"       → {6, [de Gaillon]}
//	"Avenue de la Liberte"                     → {0, [de la Liberte]}
func ParseAddress(addr string) AddressParts {
	return fraddr.Parse(addr)
}

// AddressPattern returns the legacy "stringified" pattern (= what
// ParseAddress(addr).Pattern() returns). Kept as a thin shim for the
// existing tests + callers that don't care about the structured
// number; new code should use ParseAddress directly.
func AddressPattern(addr string) string {
	return fraddr.Parse(addr).Pattern()
}

// rangeOrphanTokens are tokens left over by fraddr.Parse after the
// leading street number has been extracted from inputs like
// "75 bis rue Gambetta", "12 à 18 rue Baudin", "31 ter rue des Ecoles".
// fraddr extracts the leading numeric run only ("75", "12", "31") and
// keeps the rest of the field as-is; the connector ("bis", "ter", "à",
// "et") and any range upper-bound digit then leak into StreetTokens,
// polluting the ilike pattern. Audit 2026-05-17 found these orphans
// account for the residual range-pollution shape after the parser's
// Step 1.5 fix retired the residence-prefix leak.
var rangeOrphanTokens = map[string]bool{
	"bis":    true,
	"ter":    true,
	"quater": true,
	"à":      true,
	"a":      true,
	"et":     true,
}

// IlikePatternFor returns the BDNB-ready ilike pattern derived from
// parts. On top of fraddr.Parse output it strips a small number of
// orphan tokens that consistently leak from range-shaped inputs ("75
// bis ...", "12 à 18 ...", "1 et 3 ..."): leading bis/ter/quater
// connectors, range upper-bound digits, and the "à"/"et" links that
// would otherwise occupy a precious slot of the 3-token cap.
//
// The cleanup is intentionally minimal: we only consume tokens at the
// FRONT of StreetTokens (the orphans always sit immediately after the
// number) and stop at the first non-orphan. Bare connectors that
// happen to appear mid-name ("Schley et Saint-François", "Brieuse et
// de la Forêt") are NOT touched — those are genuinely ambiguous and
// fraddr can't disambiguate without a smarter parser.
func IlikePatternFor(parts AddressParts) string {
	toks := parts.StreetTokens
	for len(toks) > 0 {
		first := toks[0]
		if first == "" {
			toks = toks[1:]
			continue
		}
		// Bare digit run (range upper bound: "12 à 18" → "18" lands here).
		isDigit := true
		for i := 0; i < len(first); i++ {
			if first[i] < '0' || first[i] > '9' {
				isDigit = false
				break
			}
		}
		if isDigit {
			toks = toks[1:]
			continue
		}
		if rangeOrphanTokens[strings.ToLower(first)] {
			toks = toks[1:]
			continue
		}
		break
	}
	if len(toks) == 0 {
		return ""
	}
	return strings.Join(toks, " ")
}
