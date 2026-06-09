package rnc

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

// rawName is the datadir filename for the upstream raw input.
const rawName = "rnc.raw.csv"

// rawURL is the data.gouv "RNIC - Actualisation quotidienne" CSV (the daily
// "with-qpv" extract). The dated path rotates; `gazetteer refresh` resolves
// the latest resource. Bump dataVintage when refreshing.
const rawURL = "https://static.data.gouv.fr/resources/registre-national-dimmatriculation-des-coproprietes/20260530-060505/20260530-rnc-data-gouv-with-qpv.csv"

const (
	metaSource  = "Registre National d'Immatriculation des Copropriétés (ANAH, data.gouv.fr, with-qpv)"
	dataVintage = "2026-05"
)

// Upstream column headers (data.gouv "with-qpv" daily file, confirmed).
const (
	colImm     = "numero_immatriculation"
	colNom     = "nom_usage_copropriete"
	colSyndic  = "type_syndic"
	colMandat  = "mandat_en_cours"
	colVoie    = "numero_voie_adresse"
	colComp1   = "adresse_complementaire_1"
	colComp2   = "adresse_complementaire_2"
	colComp3   = "adresse_complementaire_3"
	colLon     = "longitude"
	colLat     = "latitude"
	colCoop    = "syndicat_cooperatif"
	colResServ = "residence_service"
	colLots    = "nombre_total_lots"
	colLotsH   = "nombre_lots_habitation"
	colConstr  = "periode_construction"
	colAidee   = "copro_aidee"
	colQpvCode = "code_qp_2024"
	colQpvNom  = "nom_qp_2024"

	// colInsee is the real Code Officiel Géographique (INSEE) of the commune —
	// and, for Paris/Lyon/Marseille, of the arrondissement (e.g. 75110 for
	// Paris 10e). This is the granularity the BAN normalizer emits for a
	// Listing, so it is what we key the per-INSEE candidate bucket on.
	//
	// Footgun: despite its name, the upstream "code_officiel_commune" column
	// holds the POSTAL code, not the INSEE (e.g. 75010 for Paris 10e, 06400
	// for Cannes). Keying on it leaves ~62 % of copropriétés unmatchable —
	// every commune whose code postal differs from its code INSEE. Use the
	// arrondissement column, which carries the true INSEE for every row.
	colInsee = "code_officiel_arrondissement_commune"
)

func oui(s string) bool { return strings.EqualFold(strings.TrimSpace(s), "oui") }

// transform rebuilds the processed RNC artifact from the upstream daily CSV.
// It keeps every row carrying a 5-digit official commune code, normalizes the
// reference + complementary streets, and emits a gzipped JSON Index. Financial
// and procedure columns are absent upstream and therefore not read.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	cr := csv.NewReader(dataset.BOMReader(rc))
	cr.Comma = ','
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("rnc: read header: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.TrimSpace(h)] = i
	}
	if _, ok := col[colInsee]; !ok {
		return fmt.Errorf("rnc: header missing %q: %v", colInsee, header)
	}
	get := func(rec []string, name string) string {
		if i, ok := col[name]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}

	idx := Index{Meta: Meta{Source: metaSource, DataVintage: dataVintage}}
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("rnc: read row: %w", err)
		}
		insee := get(rec, colInsee)
		if len(insee) != 5 {
			continue
		}
		lat, _ := strconv.ParseFloat(get(rec, colLat), 64)
		lon, _ := strconv.ParseFloat(get(rec, colLon), 64)
		lots, _ := strconv.Atoi(get(rec, colLots))
		lotsH, _ := strconv.Atoi(get(rec, colLotsH))

		var comp []string
		for _, c := range []string{colComp1, colComp2, colComp3} {
			if v := normVoie(get(rec, c)); v != "" {
				comp = append(comp, v)
			}
		}

		idx.Copros = append(idx.Copros, Entry{
			Immatriculation:    get(rec, colImm),
			NomUsage:           get(rec, colNom),
			INSEE:              insee,
			Lat:                lat,
			Lon:                lon,
			VoieNorm:           normVoie(get(rec, colVoie)),
			VoiesComp:          comp,
			TypeSyndic:         get(rec, colSyndic),
			MandatEnCours:      get(rec, colMandat),
			CoproAidee:         oui(get(rec, colAidee)),
			SyndicatCooperatif: oui(get(rec, colCoop)),
			ResidenceService:   oui(get(rec, colResServ)),
			LotsTotal:          lots,
			LotsHabitation:     lotsH,
			ConstructionPeriod: get(rec, colConstr),
			QPVCode:            get(rec, colQpvCode),
			QPVName:            get(rec, colQpvNom),
		})
	}
	if len(idx.Copros) == 0 {
		return errors.New("rnc: transform produced no copros")
	}
	idx.Meta.RowCount = len(idx.Copros)

	if err := dataset.WriteGzJSON(dst, &idx); err != nil {
		return fmt.Errorf("rnc: encode json: %w", err)
	}
	return nil
}

// validate gates publication: the rebuilt artifact must gunzip, parse and be
// non-empty.
func validate(r io.Reader) error {
	idx, err := parseIndex(r)
	if err != nil {
		return err
	}
	if idx.Count() == 0 {
		return errors.New("rnc: validated artifact has no copros")
	}
	return nil
}
