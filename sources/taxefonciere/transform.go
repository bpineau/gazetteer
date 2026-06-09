package taxefonciere

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/communes"
	"github.com/bpineau/gazetteer/helpers/stats"
)

// Raw input filenames (datadir basenames), upstream dataset ids, and the
// Opendatasoft JSON export URLs -----------------------------------------
//
// Both upstreams live on data.economie.gouv.fr (an Opendatasoft portal).
// The committed artifacts are reproduced from these exports (proven via
// per-file python/Go diffs: V1 34977 communes / 103 depts and V2 34919
// communes / 101 depts, all zero substantive mismatches against the
// committed snapshots).
const (
	// V1: "Taxe foncière sur les propriétés bâties - Tarifs des locaux
	// d'habitation 2024". One row per (commune, category) carrying the
	// per-m² valeur locative cadastrale (vl_au_m2). The artifact stores
	// the median vl_au_m2 across categories per commune.
	rawV1Name   = "taxe_fonciere_tarifs.raw.json"
	v1DatasetID = "descriptif-tarifs-des-locaux-d-habitation_2024"
	// The export is projected to the three columns the transform needs
	// (departement, code_commune, vl_au_m2): a ~3x smaller body than the
	// full export, keeping the download comfortably inside the refresh
	// client's request budget.
	rawV1URL       = "https://data.economie.gouv.fr/api/explore/v2.1/catalog/datasets/" + v1DatasetID + "/exports/json?select=departement,code_commune,vl_au_m2"
	v1MetaSource   = "data.economie.gouv.fr/descriptif-tarifs-des-locaux-d-habitation_2024"
	v1MetaUnit     = "vl_eur_per_m2"
	v1MetaNote     = "median valeur locative cadastrale per m² across categories per commune (residential). Multiply by taux_TFPB × 0.5 abattement to estimate TF €/m²/year."
	v1RoundDecimal = 3

	// V2: "Fiscalité locale des particuliers". One row per (commune,
	// exercice) carrying the voted global TFPB rate (taux_global_tfb) and
	// the plain TEOM rate (taux_plein_teom). The export is filtered to
	// v2Exercice (the committed snapshot vintage).
	rawV2Name      = "fiscalite_locale.raw.json"
	v2DatasetID    = "fiscalite-locale-des-particuliers"
	rawV2URL       = "https://data.economie.gouv.fr/api/explore/v2.1/catalog/datasets/" + v2DatasetID + "/exports/json?where=exercice%3D%222025%22&select=exercice,dep,com,insee_com,taux_global_tfb,taux_plein_teom"
	v2MetaSource   = "data.economie.gouv.fr/explore/dataset/fiscalite-locale-des-particuliers/ — exercice 2025"
	v2Exercice     = 2025
	v2MetaNote     = "TFPB = taux_global_tfb (taux voté commune + EPCI + syndicats). TEOM = taux_plein_teom (taux d'imposition global). Lookup multiplies tfpb_pct/100 × VLC × surface × 0.5 abattement to estimate TF €/an. Paris/Lyon/Marseille arrondissements aliased to the commune mère (75056/69123/13055) since DGFiP collects only at commune-mère level."
	v2RoundDecimal = 2

	// tfpbMaxPct drops implausibly high voted TFPB rates (a handful of
	// communes carry a taux_global_tfb above 100 %, kept out of both the
	// per-commune value and the dept median; their TEOM is still kept).
	tfpbMaxPct = 100.0
)

// transformV1 rebuilds taxe_fonciere_ratios.json from the V1 Opendatasoft
// JSON export. Each row is one (commune, category) tariff carrying
// vl_au_m2; the artifact stores the median vl_au_m2 per commune (INSEE =
// departement + zero-padded code_commune) plus a per-department median
// fallback. Dept fallback values are rounded to 3 decimals.
func transformV1(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawV1Name)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	// v1ExportRow is the upstream export shape. departement is a string
	// ("1".."95", "2A", "2B", "971"..); code_commune is an int.
	type v1ExportRow struct {
		Departement *string  `json:"departement"`
		CodeCommune *int     `json:"code_commune"`
		VLAuM2      *float64 `json:"vl_au_m2"`
	}

	var rows []v1ExportRow
	if err := json.NewDecoder(dataset.BOMReader(rc)).Decode(&rows); err != nil {
		return fmt.Errorf("taxefonciere: decode v1 export: %w", err)
	}

	byCommune := map[string][]float64{}
	for _, r := range rows {
		if r.Departement == nil || r.CodeCommune == nil || r.VLAuM2 == nil {
			continue
		}
		insee := buildINSEE(*r.Departement, *r.CodeCommune)
		if insee == "" {
			continue
		}
		byCommune[insee] = append(byCommune[insee], *r.VLAuM2)
	}
	if len(byCommune) == 0 {
		return errors.New("taxefonciere: v1 transform produced no communes")
	}

	idx := V1Index{Communes: map[string]float64{}, DeptFallback: map[string]float64{}}
	idx.Meta.Source = v1MetaSource
	idx.Meta.Unit = v1MetaUnit
	idx.Meta.Note = v1MetaNote

	byDept := map[string][]float64{}
	for insee, vls := range byCommune {
		m := stats.Median(vls)
		idx.Communes[insee] = roundDecimals(m, v1RoundDecimal)
		// The dept-median fallback is computed from the unrounded commune
		// medians (then rounded once), matching the committed artifact.
		byDept[v1DeptKey(insee)] = append(byDept[v1DeptKey(insee)], m)
	}
	for dept, ms := range byDept {
		idx.DeptFallback[dept] = roundDecimals(stats.Median(ms), v1RoundDecimal)
	}

	return json.NewEncoder(dst).Encode(idx)
}

// transformV2 rebuilds fiscalite_locale.json from the V2 Opendatasoft JSON
// export (filtered to v2Exercice). Each row carries the voted TFPB rate
// (taux_global_tfb, dropped when above tfpbMaxPct) and the plain TEOM rate
// (taux_plein_teom, omitted when absent/zero). Paris/Lyon/Marseille
// arrondissements are aliased to their commune mère. The dept fallback is
// the per-department median of each rate, rounded to 2 decimals; the
// arrondissement aliases are excluded from that median.
func transformV2(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawV2Name)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	// v2ExportRow is the upstream export shape. insee_com is the 5-digit
	// INSEE code; the rate cells are null when not voted.
	type v2ExportRow struct {
		InseeCom string   `json:"insee_com"`
		TauxTFB  *float64 `json:"taux_global_tfb"`
		TauxTEOM *float64 `json:"taux_plein_teom"`
	}

	var rows []v2ExportRow
	if err := json.NewDecoder(dataset.BOMReader(rc)).Decode(&rows); err != nil {
		return fmt.Errorf("taxefonciere: decode v2 export: %w", err)
	}

	communes := map[string]V2Entry{}
	for _, r := range rows {
		insee := strings.TrimSpace(r.InseeCom)
		if insee == "" {
			continue
		}
		var e V2Entry
		if r.TauxTFB != nil && *r.TauxTFB <= tfpbMaxPct {
			e.TFPBPct = *r.TauxTFB
		}
		if r.TauxTEOM != nil && *r.TauxTEOM != 0 {
			e.TEOMPct = *r.TauxTEOM
		}
		communes[insee] = e
	}
	if len(communes) == 0 {
		return errors.New("taxefonciere: v2 transform produced no communes")
	}

	// Alias the Paris/Lyon/Marseille arrondissements to their commune mère.
	aliases := arrondissementAliases()
	for arr, mere := range aliases {
		if e, ok := communes[mere]; ok {
			communes[arr] = e
		}
	}

	idx := V2Index{Communes: communes, DeptFallback: map[string]V2Entry{}}
	idx.Meta.Source = v2MetaSource
	idx.Meta.DownloadedAt = time.Now().UTC().Format("2006-01-02")
	idx.Meta.DataYear = v2Exercice
	idx.Meta.RowCountCommunes = len(rows)
	idx.Meta.Note = v2MetaNote

	// Dept fallback medians, computed over the real communes only (the
	// arrondissement aliases are excluded so they don't double-weight the
	// PLM commune-mère value).
	byDeptTFPB := map[string][]float64{}
	byDeptTEOM := map[string][]float64{}
	for insee, e := range communes {
		if _, isAlias := aliases[insee]; isAlias {
			continue
		}
		dept := deptFromInsee(insee)
		if dept == "" {
			continue
		}
		if e.TFPBPct > 0 {
			byDeptTFPB[dept] = append(byDeptTFPB[dept], e.TFPBPct)
		}
		if e.TEOMPct > 0 {
			byDeptTEOM[dept] = append(byDeptTEOM[dept], e.TEOMPct)
		}
	}
	depts := map[string]bool{}
	for d := range byDeptTFPB {
		depts[d] = true
	}
	for d := range byDeptTEOM {
		depts[d] = true
	}
	for d := range depts {
		var entry V2Entry
		if vs := byDeptTFPB[d]; len(vs) > 0 {
			entry.TFPBPct = roundDecimals(stats.Median(vs), v2RoundDecimal)
		}
		if vs := byDeptTEOM[d]; len(vs) > 0 {
			entry.TEOMPct = roundDecimals(stats.Median(vs), v2RoundDecimal)
		}
		idx.DeptFallback[d] = entry
	}
	idx.Meta.RowCountDepts = len(idx.DeptFallback)

	applyV2Defaults(&idx)

	return json.NewEncoder(dst).Encode(idx)
}

// validateV1 gates publication: the rebuilt V1 artifact must parse and
// carry communes.
func validateV1(r io.Reader) error {
	var idx V1Index
	if err := json.NewDecoder(r).Decode(&idx); err != nil {
		return fmt.Errorf("taxefonciere: validate v1: %w", err)
	}
	if len(idx.Communes) == 0 {
		return errors.New("taxefonciere: validated v1 artifact has no communes")
	}
	return nil
}

// validateV2 gates publication: the rebuilt V2 artifact must parse and
// carry communes.
func validateV2(r io.Reader) error {
	var idx V2Index
	if err := json.NewDecoder(r).Decode(&idx); err != nil {
		return fmt.Errorf("taxefonciere: validate v2: %w", err)
	}
	if len(idx.Communes) == 0 {
		return errors.New("taxefonciere: validated v2 artifact has no communes")
	}
	return nil
}

// buildINSEE reconstructs the 5/6-char INSEE code from the upstream V1
// (departement, code_commune) pair: departement left-zero-padded to 2
// digits (Corsica "2A"/"2B" kept as-is, DOM departements already 3-char)
// followed by the commune code zero-padded to 3 digits.
func buildINSEE(dept string, code int) string {
	dept = strings.TrimSpace(dept)
	if dept == "" {
		return ""
	}
	if dept != "2A" && dept != "2B" && len(dept) < 2 {
		dept = strings.Repeat("0", 2-len(dept)) + dept
	}
	return fmt.Sprintf("%s%03d", dept, code)
}

// v1DeptKey is the V1 dept-fallback grouping key: Corsica communes
// (2A.../2B...) group under the 3-char prefix (matching the committed V1
// snapshot), every other commune under its 2-char prefix (so the DOM
// 971.../972... all fold into "97").
func v1DeptKey(insee string) string {
	if strings.HasPrefix(insee, "2A") || strings.HasPrefix(insee, "2B") {
		if len(insee) >= 3 {
			return insee[:3]
		}
	}
	if len(insee) >= 2 {
		return insee[:2]
	}
	return insee
}

// arrondissementAliases maps each Paris/Lyon/Marseille arrondissement
// INSEE code to its commune-mère INSEE code. DGFiP collects rates only at
// commune-mère level, so each arrondissement inherits the mère's entry.
func arrondissementAliases() map[string]string {
	return communes.ArrondissementParents()
}

// roundDecimals rounds x to dp decimal places using correctly-rounded
// decimal formatting (round-half-to-even on the true decimal value),
// matching the committed artifacts' Python-`round` semantics. Going
// through strconv avoids the off-by-one a naïve x*10^dp multiply would
// introduce on values like 40.735 (which is really 40.73499…).
func roundDecimals(x float64, dp int) float64 {
	f, err := strconv.ParseFloat(strconv.FormatFloat(x, 'f', dp, 64), 64)
	if err != nil {
		return x
	}
	return f
}
