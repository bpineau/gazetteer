package delinquance

import (
	"compress/gzip"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/frnorm"
	"github.com/bpineau/gazetteer/helpers/stats"
)

// rawName is the datadir filename for the upstream raw input. The upstream
// ships a single large gzipped CSV; we keep it gzipped on disk (the
// transform decompresses it on the fly).
const rawName = "delinquance_communale.raw.csv.gz"

// rawURL is the SSMSI "Base statistique communale de la délinquance
// enregistrée par la police et la gendarmerie nationales" CSV (gzip),
// published on data.gouv.fr (dataset slug
// bases-statistiques-communale-departementale-et-regionale-…). data.gouv
// mints a dated static path per revision; bump this — together with
// targetYear below — when SSMSI publishes a new reference-year edition (the
// slug page lists the current "COM … fichier csv compressé" resource).
const rawURL = "https://static.data.gouv.fr/resources/bases-statistiques-communale-departementale-et-regionale-de-la-delinquance-enregistree-par-la-police-et-la-gendarmerie-nationales/20260326-124144/donnee-data.gouv-2025-geographie2025-produit-le2026-02-03.csv.gz"

// metaSource is the provenance string recorded in the rebuilt artifact.
const metaSource = "data.gouv.fr SSMSI bases statistiques communale"

// metaUnit / metaNote mirror the snapshot's meta. The note documents the
// per-1000 normalisation and the ndiff (3-year smoothed) fallback.
const (
	metaUnit = "per_thousand"
	metaNote = "Rates per 1000 inhabitants (or per 1000 logements for burglary). ndiff rows use the SSMSI 3-year smoothed rate."
)

// targetYear is the reference year extracted from the multi-year upstream
// CSV (which carries every year from 2016 onward). It is pinned, like
// rawURL, to the published snapshot; bump both when refreshing to a newer
// edition.
const targetYear = 2024

// CSV column headers (semicolon-separated, BOM-prefixed). The geo column is
// suffixed with the geography millésime ("CODGEO_2025"); it is matched by
// the "CODGEO" prefix so an edition bump does not break the mapping.
const (
	colAnnee       = "annee"
	colIndicateur  = "indicateur"
	colNombre      = "nombre"
	colTaux        = "taux_pour_mille"
	colEstDiffuse  = "est_diffuse"
	colInseePop    = "insee_pop"
	colComplTaux   = "complement_info_taux"
	colCodgeoPfx   = "CODGEO"
	diffPublished  = "diff"
	diffSuppressed = "ndiff"
)

// label2handle maps the upstream French "indicateur" label to the short
// English handle stored in the artifact. The 15th upstream category
// "Usage de stupéfiants (AFD)" (forfait-délictuel amende) is intentionally
// excluded: drug_use carries only the "Usage de stupéfiants" headline.
var label2handle = map[string]string{
	"Violences physiques intrafamiliales":            "violence_family",
	"Violences physiques hors cadre familial":        "violence_outside_family",
	"Violences sexuelles":                            "sexual_violence",
	"Vols avec armes":                                "robbery_armed",
	"Vols violents sans arme":                        "robbery_unarmed",
	"Vols sans violence contre des personnes":        "theft_no_violence",
	"Cambriolages de logement":                       "burglary",
	"Vols de véhicule":                               "vehicle_theft",
	"Vols dans les véhicules":                        "vehicle_break_in",
	"Vols d'accessoires sur véhicules":               "vehicle_accessory_theft",
	"Destructions et dégradations volontaires":       "vandalism",
	"Usage de stupéfiants":                           "drug_use",
	"Trafic de stupéfiants":                          "drug_trafficking",
	"Escroqueries et fraudes aux moyens de paiement": "fraud",
}

// transform rebuilds the processed delinquance artifact from the upstream
// SSMSI commune CSV. For each (commune, indicator) row of targetYear it
// records the per-1000 rate: the commune's own published rate
// (est_diffuse="diff" → taux_pour_mille) or, when suppressed by the
// statistical-secret rule (est_diffuse="ndiff"), the SSMSI 3-year smoothed
// fallback (complement_info_taux). Population is insee_pop. The result is
// gzip+JSON, matching the committed on-disk format.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	zr, err := gzip.NewReader(rc)
	if err != nil {
		return fmt.Errorf("delinquance: gunzip raw: %w", err)
	}
	defer func() { _ = zr.Close() }()

	cr := csv.NewReader(dataset.BOMReader(zr))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("delinquance: read header: %w", err)
	}
	col := map[string]int{}
	codgeo := -1
	for i, h := range header {
		h = strings.TrimSpace(h)
		col[h] = i
		if codgeo < 0 && strings.HasPrefix(h, colCodgeoPfx) {
			codgeo = i
		}
	}
	need := func(name string) (int, error) {
		i, ok := col[name]
		if !ok {
			return 0, fmt.Errorf("delinquance: header missing column %q: %v", name, header)
		}
		return i, nil
	}
	annee, e1 := need(colAnnee)
	indCol, e2 := need(colIndicateur)
	taux, e3 := need(colTaux)
	estDiff, e4 := need(colEstDiffuse)
	pop, e5 := need(colInseePop)
	complTaux, e6 := need(colComplTaux)
	for _, e := range []error{e1, e2, e3, e4, e5, e6} {
		if e != nil {
			return e
		}
	}
	if codgeo < 0 {
		return fmt.Errorf("delinquance: header missing CODGEO column: %v", header)
	}

	year := strconv.Itoa(targetYear)
	idx := Index{
		Meta: Meta{
			Source:   metaSource,
			DataYear: targetYear,
			Unit:     metaUnit,
			Note:     metaNote,
		},
		Communes: map[string]Entry{},
	}
	handles := map[string]bool{}

	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("delinquance: read row: %w", err)
		}
		if strings.TrimSpace(rec[annee]) != year {
			continue
		}
		handle, ok := label2handle[strings.TrimSpace(rec[indCol])]
		if !ok {
			continue
		}
		rate, ok := pickRate(rec[estDiff], rec[taux], rec[complTaux])
		if !ok {
			continue
		}
		insee := strings.TrimSpace(rec[codgeo])
		if insee == "" {
			continue
		}
		e, ok := idx.Communes[insee]
		if !ok {
			e = Entry{Rates: map[string]float64{}}
		}
		if p, err := strconv.Atoi(strings.TrimSpace(rec[pop])); err == nil {
			e.Population = p
		}
		// 4 decimals matches the snapshot's stored precision.
		e.Rates[handle] = stats.Round(rate, 4)
		idx.Communes[insee] = e
		handles[handle] = true
	}

	if len(idx.Communes) == 0 {
		return errors.New("delinquance: transform produced no communes")
	}
	idx.Meta.RowCountCommunes = len(idx.Communes)
	idx.Meta.Indicators = sortedKeys(handles)

	if err := dataset.WriteGzJSON(dst, idx); err != nil {
		return fmt.Errorf("delinquance: encode: %w", err)
	}
	return nil
}

// pickRate returns the per-1000 rate for a row: the published
// taux_pour_mille when the commune's count is diffused, else the SSMSI
// 3-year smoothed fallback (complement_info_taux) for suppressed rows.
// Returns ok=false when neither carries a parseable number.
func pickRate(estDiff, taux, complTaux string) (float64, bool) {
	switch strings.TrimSpace(estDiff) {
	case diffPublished:
		return parseFR(taux)
	case diffSuppressed:
		return parseFR(complTaux)
	default:
		// Unknown flag: prefer the published rate, fall back to the smoothed.
		if v, ok := parseFR(taux); ok {
			return v, true
		}
		return parseFR(complTaux)
	}
}

// parseFR parses an SSMSI decimal ("1,3620690", comma decimal separator).
// "NA"/empty yield ok=false.
func parseFR(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "NA" {
		return 0, false
	}
	return frnorm.ParseFRFloat(s)
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// validate gates publication: the rebuilt artifact must gunzip, parse and be
// non-empty.
func validate(r io.Reader) error {
	idx, err := parseIndex(r)
	if err != nil {
		return err
	}
	if idx.Count() == 0 {
		return errors.New("delinquance: validated artifact has no communes")
	}
	return nil
}
