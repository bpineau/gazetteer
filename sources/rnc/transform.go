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
// "with-qpv" extract), via the STABLE resource permalink: the underlying
// dated static.data.gouv.fr path rotates every day, so pinning a dated
// URL 404s within days (it did). The permalink always redirects to the
// current file. Bump dataVintage when refreshing.
const rawURL = "https://www.data.gouv.fr/fr/datasets/r/3ea8e2c3-0038-464a-b17e-cd5c91f65ce2"

const (
	metaSource  = "Registre National d'Immatriculation des Copropriétés (ANAH, data.gouv.fr, with-qpv)"
	dataVintage = "2026-07"
)

// Upstream column headers (data.gouv "with-qpv" daily file, confirmed).
const (
	colImm       = "numero_immatriculation"
	colNom       = "nom_usage_copropriete"
	colSyndic    = "type_syndic"
	colMandat    = "mandat_en_cours"
	colMandatFin = "date_fin_dernier_mandat"
	colVoie      = "numero_voie_adresse"
	colComp1     = "adresse_complementaire_1"
	colComp2     = "adresse_complementaire_2"
	colComp3     = "adresse_complementaire_3"
	colLon       = "longitude"
	colLat       = "latitude"
	colCoop      = "syndicat_cooperatif"
	colResServ   = "residence_service"
	colLots      = "nombre_total_lots"
	colLotsH     = "nombre_lots_habitation"
	colLotsP     = "nombre_lots_stationnement"
	colConstr    = "periode_construction"
	colCad1      = "reference_cadastrale_1"
	colCad2      = "reference_cadastrale_2"
	colCad3      = "reference_cadastrale_3"
	colAidee     = "copro_aidee"
	colACV       = "copro_dans_acv"
	colPVD       = "copro_dans_pvd"
	colPDP       = "copro_dans_pdp"
	colQpvCode   = "code_qp_2024"
	colQpvNom    = "nom_qp_2024"

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
// reference + complementary streets, reads the cadastral parcelles, the last
// mandate end date and the public-programme flags, and emits a gzipped JSON
// Index. The financial declarations and the legal-procedure/arrêté columns are
// redacted from the open-data export and therefore cannot be read.
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
		lotsP, _ := strconv.Atoi(get(rec, colLotsP))

		var comp []string
		for _, c := range []string{colComp1, colComp2, colComp3} {
			if v := normVoie(get(rec, c)); v != "" {
				comp = append(comp, v)
			}
		}

		var parcelles []string
		for _, c := range []string{colCad1, colCad2, colCad3} {
			if v := get(rec, c); v != "" {
				parcelles = append(parcelles, v)
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
			MandatFin:          get(rec, colMandatFin),
			CoproAidee:         oui(get(rec, colAidee)),
			SyndicatCooperatif: oui(get(rec, colCoop)),
			ResidenceService:   oui(get(rec, colResServ)),
			LotsTotal:          lots,
			LotsHabitation:     lotsH,
			LotsStationnement:  lotsP,
			ConstructionPeriod: get(rec, colConstr),
			Parcelles:          parcelles,
			DansACV:            oui(get(rec, colACV)),
			DansPVD:            oui(get(rec, colPVD)),
			DansPDP:            oui(get(rec, colPDP)),
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
	idx, err := parseIndexStream(r, nil)
	if err != nil {
		return err
	}
	if idx.Count() == 0 {
		return errors.New("rnc: validated artifact has no copros")
	}
	return nil
}
