// Package bdnb is a gazetteer.Source that pulls a building's
// characterisation from BDNB — the Base de Données Nationale des
// Bâtiments published by CSTB.
//
// # Strategy
//
// BDNB exposes a public PostgREST API at `api.bdnb.io`. The endpoint
//
//	GET /v1/bdnb/donnees/batiment_groupe_complet
//
// returns a per-batiment-groupe row with 100+ pre-aggregated columns
// (DPE, RNB id, parcelle cadastrale, distance MH, contrainte ABF,
// IRIS, etc.). 10 000 requests per rolling 30-day window are free, no
// API key required.
//
// # Filtering
//
// The non-indexed PostgREST filters time out (HTTP 500 / 57014). The
// Source always scopes by `code_commune_insee` (indexed) and adds
// either the BAN id (`cle_interop_adr_principale_ban`) or an `ilike`
// pattern on `libelle_adr_principale_ban` as the second filter. See
// url.go.
//
// # Rhythm & rate-limit
//
// Standard rhythm. 1 req/s on `api.bdnb.io` (per-host) — well under
// the ~330 req/day budget allowed by the 10k/30d quota.
package bdnb

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrEmptyBody is returned by ParseList when the input is empty or
// not parseable as JSON. The Source wraps it as
// gazetteer.ErrUpstreamUnavailable.
var ErrEmptyBody = errors.New("bdnb: empty / unparseable body")

// flexString is a tolerant JSON decoder that accepts string, bool,
// number or null. Used for BDNB columns whose declared type drifts
// across rows (e.g. `quartier_prioritaire` returns null on most rows
// and `true`/`false` when the building actually is in a QPV).
//
// String form mapping:
//
//	null    → ""
//	"foo"   → "foo"
//	true    → "true"
//	false   → "false"
//	42      → "42"
//	42.5    → "42.5"
type flexString string

// String returns the underlying string value. Convenience for callers
// that want a plain `string` even though the Go zero value of
// flexString already prints fine.
func (s flexString) String() string { return string(s) }

// UnmarshalJSON implements json.Unmarshaler.
func (s *flexString) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		*s = ""
		return nil
	}
	switch b[0] {
	case '"':
		var v string
		if err := json.Unmarshal(b, &v); err != nil {
			*s = ""
			return nil
		}
		*s = flexString(v)
	case 't':
		*s = "true"
	case 'f':
		*s = "false"
	case '[':
		// L8 — JSON array. We don't have any consumer that expects a list
		// here (BDNB exposes flexString fields as scalar columns), but the
		// API has been observed to occasionally return a 1-element array
		// (e.g. `["A"]`) for `classe_bilan_dpe`. Unwrap to the first
		// element's string form ; treat empty arrays as null.
		var arr []json.RawMessage
		if err := json.Unmarshal(b, &arr); err != nil || len(arr) == 0 {
			*s = ""
			return nil
		}
		// Recurse on the first element so we share the scalar-handling
		// branches above for whatever type the API stuffed inside.
		var inner flexString
		if err := inner.UnmarshalJSON(arr[0]); err != nil {
			*s = ""
			return nil
		}
		*s = inner
	case '{':
		// Object — never observed in practice but defensible: store the
		// compact JSON form as a fallback rather than crashing.
		*s = flexString(strings.TrimSpace(string(b)))
	default:
		// Number / unknown: keep the raw representation, trimmed.
		*s = flexString(strings.TrimSpace(string(b)))
	}
	return nil
}

// Row is the trimmed, typed shape of a single BDNB
// `batiment_groupe_complet` response row. Only the fields the Source
// renders into the Result are kept; the rest of the 100+ columns are
// ignored by encoding/json.
//
// Numeric columns that BDNB sometimes returns as float (DPE consos)
// are decoded as `*float64`; counts are decoded as `*int`. All
// pointers stay nil when the field is absent / null in the JSON, so
// downstream code can distinguish "0" from "unknown".
type Row struct {
	// Identity
	BatimentGroupeID        string   `json:"batiment_groupe_id"`
	CleInteropAdrPrincipale string   `json:"cle_interop_adr_principale_ban"`
	LibelleAdrPrincipale    string   `json:"libelle_adr_principale_ban"`
	CodeCommuneINSEE        string   `json:"code_commune_insee"`
	CodeIRIS                string   `json:"code_iris"`
	CodeEPCI                string   `json:"code_epci_insee"`
	CodeDepartement         string   `json:"code_departement_insee"`
	LParcelleID             []string `json:"l_parcelle_id"`

	// Building
	AnneeConstruction      *int   `json:"annee_construction"`
	AnneeConstructionDPE   *int   `json:"annee_construction_dpe"`
	NbLog                  *int   `json:"nb_log"`
	NbNiveau               *int   `json:"nb_niveau"`
	SurfaceEmpriseSol      *int   `json:"surface_emprise_sol"`
	HauteurMean            *int   `json:"hauteur_mean"`
	UsagePrincipalBdnbOpen string `json:"usage_principal_bdnb_open"`
	TypeBatimentDPE        string `json:"type_batiment_dpe"`

	// DPE
	ClasseBilanDPE                   string   `json:"classe_bilan_dpe"`
	ClasseConsoEnergieArrete2012     string   `json:"classe_conso_energie_arrete_2012"`
	Conso3UsagesEpM2Arrete2012       *float64 `json:"conso_3_usages_ep_m2_arrete_2012"`
	Conso5UsagesEpM2                 *float64 `json:"conso_5_usages_ep_m2"`
	EmissionGES3UsagesEpM2Arrete2012 *float64 `json:"emission_ges_3_usages_ep_m2_arrete_2012"`
	EmissionGES5UsagesM2             *float64 `json:"emission_ges_5_usages_m2"`
	TypeEnergieChauffage             string   `json:"type_energie_chauffage"`
	TypeGenerateurChauffage          string   `json:"type_generateur_chauffage"`
	TypeIsolationMurExterieur        string   `json:"type_isolation_mur_exterieur"`
	TypeVitrage                      string   `json:"type_vitrage"`
	NbClasseBilanDPEA                *int     `json:"nb_classe_bilan_dpe_a"`
	NbClasseBilanDPEB                *int     `json:"nb_classe_bilan_dpe_b"`
	NbClasseBilanDPEC                *int     `json:"nb_classe_bilan_dpe_c"`
	NbClasseBilanDPED                *int     `json:"nb_classe_bilan_dpe_d"`
	NbClasseBilanDPEE                *int     `json:"nb_classe_bilan_dpe_e"`
	NbClasseBilanDPEF                *int     `json:"nb_classe_bilan_dpe_f"`
	NbClasseBilanDPEG                *int     `json:"nb_classe_bilan_dpe_g"`
	NbClasseConsoEnergieArrete2012A  *int     `json:"nb_classe_conso_energie_arrete_2012_a"`
	NbClasseConsoEnergieArrete2012B  *int     `json:"nb_classe_conso_energie_arrete_2012_b"`
	NbClasseConsoEnergieArrete2012C  *int     `json:"nb_classe_conso_energie_arrete_2012_c"`
	NbClasseConsoEnergieArrete2012D  *int     `json:"nb_classe_conso_energie_arrete_2012_d"`
	NbClasseConsoEnergieArrete2012E  *int     `json:"nb_classe_conso_energie_arrete_2012_e"`
	NbClasseConsoEnergieArrete2012F  *int     `json:"nb_classe_conso_energie_arrete_2012_f"`
	NbClasseConsoEnergieArrete2012G  *int     `json:"nb_classe_conso_energie_arrete_2012_g"`
	NbClasseConsoEnergieArrete2012NC *int     `json:"nb_classe_conso_energie_arrete_2012_nc"`

	// Risks / urbanism
	DistanceMonumentHistorique      *int       `json:"distance_monument_historique"`
	NomBatimentHistoriquePlusProche string     `json:"nom_batiment_historique_plus_proche"`
	PerimetreBatHistorique          *bool      `json:"perimetre_bat_historique"`
	ContrainteUrbanismeAC1          *bool      `json:"contrainte_urbanisme_ac1"`
	ZonePLUBatiPatrimonial          *bool      `json:"zone_plu_bati_patrimonial"`
	QuartierPrioritaire             flexString `json:"quartier_prioritaire"`
	NomQP                           string     `json:"nom_qp"`
	NomQuartierQPV                  string     `json:"nom_quartier_qpv"`
	ValeurFonciereM2RelCommune      *float64   `json:"valeur_fonciere_m2_residentiel_rel_commune"`

	// Reliability
	FiabiliteCRAdrNiv1 string `json:"fiabilite_cr_adr_niv_1"`
	FiabiliteCRAdrNiv2 string `json:"fiabilite_cr_adr_niv_2"`
}

// ParseList decodes a PostgREST-style JSON array of `batiment_groupe_complet`
// rows into the trimmed Row shape. Returns ErrEmptyBody on an
// unparseable body. An empty array `[]` is returned without error so
// callers can distinguish "no results" from a parser failure.
func ParseList(body []byte) ([]Row, error) {
	if len(body) == 0 {
		return nil, ErrEmptyBody
	}
	var rows []Row
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrEmptyBody, err)
	}
	return rows, nil
}

// PickBest returns the index of the best matching row for the given
// `wantBANID`. When `wantBANID` is non-empty, the row whose
// `cle_interop_adr_principale_ban` matches exactly wins. Otherwise the
// row with the highest "completeness" (= more non-null business fields)
// wins. Returns (-1, false) on an empty list.
//
// We never select "the last" or "an arbitrary" row — when several
// rows are returned (e.g. an `ilike` query returning multiple
// buildings on the same street), we want a deterministic choice.
func PickBest(rows []Row, wantBANID string) (int, bool) {
	if len(rows) == 0 {
		return -1, false
	}
	want := strings.TrimSpace(wantBANID)
	if want != "" {
		for i, r := range rows {
			if strings.EqualFold(r.CleInteropAdrPrincipale, want) {
				return i, true
			}
		}
	}
	// Fallback: pick the row with the highest completeness score.
	bestIdx := 0
	bestScore := completenessScore(rows[0])
	for i := 1; i < len(rows); i++ {
		s := completenessScore(rows[i])
		if s > bestScore {
			bestScore = s
			bestIdx = i
		}
	}
	return bestIdx, true
}

// PickBestByNumber tries to find the row whose libelle_adr starts with
// `wantNum` followed by a non-digit. This is the post-filter applied
// when an `ilike` query returned several rows on the same street and
// the listing had a known street number — we want "9 RUE AUBERT", not
// "8 RUE AUBERT". Returns (-1, false) when no row matches; callers
// then fall back to PickBest.
//
// `wantNum` is matched as the leading digit run of the row's libelle,
// so "12" matches "12 RUE" but not "120 RUE".
func PickBestByNumber(rows []Row, wantNum string) (int, bool) {
	wantNum = strings.TrimSpace(wantNum)
	if wantNum == "" {
		return -1, false
	}
	for i, r := range rows {
		got := extractLeadingNumberFromLibelle(r.LibelleAdrPrincipale)
		if got == wantNum {
			return i, true
		}
	}
	return -1, false
}

// extractLeadingNumberFromLibelle returns the leading digit run of a
// BDNB `libelle_adr_principale_ban` value. Matches the same logic as
// url.extractLeadingNumber but operates on whole libelles. Stops at
// the first non-digit (so "120 RUE" gives "120", "12B RUE" gives "12").
func extractLeadingNumberFromLibelle(libelle string) string {
	libelle = strings.TrimSpace(libelle)
	end := 0
	for end < len(libelle) && libelle[end] >= '0' && libelle[end] <= '9' {
		end++
	}
	return libelle[:end]
}

// completenessScore counts the number of non-null / non-empty fields
// that matter for the rendered payload. Used as a tiebreaker by
// PickBest.
func completenessScore(r Row) int {
	score := 0
	if r.BatimentGroupeID != "" {
		score++
	}
	if r.AnneeConstruction != nil {
		score++
	}
	if r.NbLog != nil {
		score++
	}
	if r.SurfaceEmpriseSol != nil {
		score++
	}
	if r.ClasseBilanDPE != "" || r.ClasseConsoEnergieArrete2012 != "" {
		score++
	}
	if r.TypeEnergieChauffage != "" {
		score++
	}
	if r.DistanceMonumentHistorique != nil {
		score++
	}
	if r.ContrainteUrbanismeAC1 != nil {
		score++
	}
	if len(r.LParcelleID) > 0 {
		score++
	}
	return score
}

// PickConfidence implements the spec's confidence calibration:
//
//	high   : a row matched by exact BAN id, fiabilite reported as
//	         "fiable" (case-insensitive substring).
//	medium : a row was matched but either the strategy was the ilike
//	         fallback or the fiabilite is degraded.
//	low    : no row matched (caller wraps in an empty payload).
func PickConfidence(matched bool, banExactHit bool, fiabilite string) string {
	if !matched {
		return ConfidenceLow
	}
	flow := strings.ToLower(strings.TrimSpace(fiabilite))
	// "donnees croisees a l'adresse fiables" → reliable.
	// "non fiable" / "douteuses" → degraded. We treat the explicit
	// "non …" / "douteus" markers as a hard negative and only count
	// "fiable" / "fiables" as reliable when those markers are absent.
	reliable := strings.Contains(flow, "fiable") &&
		!strings.Contains(flow, "non ") &&
		!strings.Contains(flow, "douteus") &&
		!strings.Contains(flow, "incoherent")
	if banExactHit && reliable {
		return ConfidenceHigh
	}
	if banExactHit {
		return ConfidenceMedium
	}
	if reliable {
		return ConfidenceMedium
	}
	return ConfidenceLow
}

// BuildResult renders a Row into the typed Result struct (excluding the
// Confidence / SampleSize / Skipped / Evidence fields, which are set by
// the caller). Pure function — exposed so callers that drive their own
// HTTP transport (e.g. a quota-tripped fetcher) can reuse the same
// row→Result projection without invoking Source.Query.
func BuildResult(r Row) *Result {
	out := &Result{}

	if r.BatimentGroupeID != "" || r.LibelleAdrPrincipale != "" {
		ident := Identity{
			BatimentGroupeID: r.BatimentGroupeID,
			BANID:            r.CleInteropAdrPrincipale,
			LibelleAdresse:   r.LibelleAdrPrincipale,
			CodeIRIS:         r.CodeIRIS,
			CodeEPCI:         r.CodeEPCI,
			CodeDepartement:  r.CodeDepartement,
		}
		if len(r.LParcelleID) > 0 {
			ident.Parcelles = append([]string(nil), r.LParcelleID...)
		}
		out.Identity = &ident
	}

	bld := Building{
		AnneeConstruction:    r.AnneeConstruction,
		AnneeConstructionDPE: r.AnneeConstructionDPE,
		TypeBatimentDPE:      r.TypeBatimentDPE,
		UsagePrincipal:       r.UsagePrincipalBdnbOpen,
		NbLog:                r.NbLog,
		NbNiveau:             r.NbNiveau,
		SurfaceEmpriseSolM2:  r.SurfaceEmpriseSol,
		HauteurMeanM:         r.HauteurMean,
	}
	if !buildingEmpty(bld) {
		out.Building = &bld
	}

	dpe := DPE{
		ClasseBilan:               r.ClasseBilanDPE,
		ClasseArrete2012:          r.ClasseConsoEnergieArrete2012,
		ConsoKwhEpM2An:            firstFloat(r.Conso5UsagesEpM2, r.Conso3UsagesEpM2Arrete2012),
		GESKgCO2M2An:              firstFloat(r.EmissionGES5UsagesM2, r.EmissionGES3UsagesEpM2Arrete2012),
		TypeEnergieChauffage:      r.TypeEnergieChauffage,
		TypeGenerateurChauffage:   r.TypeGenerateurChauffage,
		TypeIsolationMurExterieur: r.TypeIsolationMurExterieur,
		TypeVitrage:               r.TypeVitrage,
		DistributionClasses:       buildDistribution(r),
	}
	if !dpeEmpty(dpe) {
		out.DPE = &dpe
	}

	risk := Risks{
		MonumentHistoriqueM:      r.DistanceMonumentHistorique,
		NomMonumentPlusProche:    r.NomBatimentHistoriquePlusProche,
		PerimetreMH:              r.PerimetreBatHistorique,
		ContrainteABFAC1:         r.ContrainteUrbanismeAC1,
		ZonePLUBatiPatrimonial:   r.ZonePLUBatiPatrimonial,
		QuartierPrioritaire:      string(r.QuartierPrioritaire),
		NomQP:                    firstStr(r.NomQP, r.NomQuartierQPV),
		ValeurFonciereRelativeM2: r.ValeurFonciereM2RelCommune,
	}
	if !risksEmpty(risk) {
		out.Risks = &risk
	}

	if r.FiabiliteCRAdrNiv1 != "" || r.FiabiliteCRAdrNiv2 != "" {
		out.Fiabilite = &Fiabilite{
			CRAdrNiv1: r.FiabiliteCRAdrNiv1,
			CRAdrNiv2: r.FiabiliteCRAdrNiv2,
		}
	}

	return out
}

// buildDistribution merges the two `nb_classe_*` families (arrete 2012
// + bilan 2021) into a single A..G+NC dictionary. We prefer the bilan
// 2021 counts when non-zero, else fall back to the 2012 arrete.
func buildDistribution(r Row) map[string]int {
	use2021 := firstNonZero(r.NbClasseBilanDPEA, r.NbClasseBilanDPEB, r.NbClasseBilanDPEC,
		r.NbClasseBilanDPED, r.NbClasseBilanDPEE, r.NbClasseBilanDPEF, r.NbClasseBilanDPEG) > 0
	out := map[string]int{}
	if use2021 {
		setIfNonNil(out, "A", r.NbClasseBilanDPEA)
		setIfNonNil(out, "B", r.NbClasseBilanDPEB)
		setIfNonNil(out, "C", r.NbClasseBilanDPEC)
		setIfNonNil(out, "D", r.NbClasseBilanDPED)
		setIfNonNil(out, "E", r.NbClasseBilanDPEE)
		setIfNonNil(out, "F", r.NbClasseBilanDPEF)
		setIfNonNil(out, "G", r.NbClasseBilanDPEG)
	} else {
		setIfNonNil(out, "A", r.NbClasseConsoEnergieArrete2012A)
		setIfNonNil(out, "B", r.NbClasseConsoEnergieArrete2012B)
		setIfNonNil(out, "C", r.NbClasseConsoEnergieArrete2012C)
		setIfNonNil(out, "D", r.NbClasseConsoEnergieArrete2012D)
		setIfNonNil(out, "E", r.NbClasseConsoEnergieArrete2012E)
		setIfNonNil(out, "F", r.NbClasseConsoEnergieArrete2012F)
		setIfNonNil(out, "G", r.NbClasseConsoEnergieArrete2012G)
		setIfNonNil(out, "NC", r.NbClasseConsoEnergieArrete2012NC)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func setIfNonNil(m map[string]int, k string, v *int) {
	if v == nil {
		return
	}
	m[k] = *v
}

func firstNonZero(vals ...*int) int {
	for _, v := range vals {
		if v != nil && *v > 0 {
			return *v
		}
	}
	return 0
}

func firstFloat(a, b *float64) *float64 {
	if a != nil {
		return a
	}
	return b
}

func firstStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func buildingEmpty(b Building) bool {
	return b.AnneeConstruction == nil && b.AnneeConstructionDPE == nil &&
		b.TypeBatimentDPE == "" && b.UsagePrincipal == "" &&
		b.NbLog == nil && b.NbNiveau == nil &&
		b.SurfaceEmpriseSolM2 == nil && b.HauteurMeanM == nil
}

func dpeEmpty(d DPE) bool {
	return d.ClasseBilan == "" && d.ClasseArrete2012 == "" &&
		d.ConsoKwhEpM2An == nil && d.GESKgCO2M2An == nil &&
		d.TypeEnergieChauffage == "" && d.TypeGenerateurChauffage == "" &&
		d.TypeIsolationMurExterieur == "" && d.TypeVitrage == "" &&
		len(d.DistributionClasses) == 0
}

func risksEmpty(r Risks) bool {
	return r.MonumentHistoriqueM == nil && r.NomMonumentPlusProche == "" &&
		r.PerimetreMH == nil && r.ContrainteABFAC1 == nil &&
		r.ZonePLUBatiPatrimonial == nil && r.QuartierPrioritaire == "" &&
		r.NomQP == "" && r.ValeurFonciereRelativeM2 == nil
}
