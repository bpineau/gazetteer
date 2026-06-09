package bpe

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

// rawName is the datadir filename for the upstream raw input — INSEE's
// BPE 2024 "dénombrement" ZIP (a CSV pair inside).
const rawName = "bpe_csv.zip"

// rawURL is the INSEE BPE 2024 general equipment-count file
// (DS_BPE_CSV_FR.zip), published under the "Dénombrement et
// géolocalisation des équipements en 2024" release. The ZIP holds
// DS_BPE_2024_data.csv (one row per GEO × FACILITY_TYPE with a count)
// and a metadata CSV. Bump this when INSEE publishes a new BPE vintage
// (and the Source's referenceDate/note with it).
const rawURL = "https://www.insee.fr/fr/statistiques/fichier/8217527/DS_BPE_CSV_FR.zip"

// csvMemberName is the data CSV inside the ZIP. The archive also carries
// DS_BPE_2024_metadata.csv (code labels), which the transform ignores.
const csvMemberName = "DS_BPE_2024_data.csv"

// metaSource is the provenance string recorded in the rebuilt artifact —
// kept byte-identical to the committed embed so a refresh is a no-op diff.
const metaSource = "INSEE BPE 2024 — dénombrement des équipements (curated bucket subset)"

// referenceDate is the BPE vintage reference date (1 January of the
// dénombrement year). The data CSV carries TIME_PERIOD=2024 inline but not
// a full date; keep this in sync with the upstream release.
const referenceDate = "2024-01-01"

// metaNote documents the artifact semantics; byte-identical to the embed.
const metaNote = "Curated rental-investor subset: services, commerce, santé, petite enfance, éducation, transport, sport. Equipment counts aggregated per commune."

// bucketByFacilityType maps each INSEE BPE FACILITY_TYPE (TYPEQU) code to
// the curated Bucket it rolls up into. This is the authoritative mapping
// documented in doc.go ("Bucket → BPE FACILITY_TYPE mapping"); the
// transform and that doc table must stay in lock-step. Codes absent here
// are outside the curated subset and dropped.
var bucketByFacilityType = map[string]Bucket{
	// Services
	"A206": BucketPoste, // Bureau de poste
	"A208": BucketPoste, // Agence postale
	// Commerce
	"B104": BucketGrandeSurface, // Hypermarché
	"B105": BucketGrandeSurface, // Supermarché
	"B201": BucketSuperette,     // Supérette
	"B207": BucketBoulangerie,   // Boulangerie-pâtisserie
	// Éducation
	"C107": BucketEcolePrimaire, // École maternelle
	"C108": BucketEcolePrimaire, // École élémentaire
	"C201": BucketCollege,       // Collège
	"C301": BucketLycee,         // Lycée d'enseignement général/technologique
	"C302": BucketLycee,         // Lycée d'enseignement professionnel
	// Santé
	"D106": BucketStructureSante,     // Urgences
	"D108": BucketStructureSante,     // Centre de santé
	"D113": BucketStructureSante,     // Maison de santé pluridisciplinaire
	"D265": BucketMedecinGeneraliste, // Médecin généraliste
	"D281": BucketInfirmier,          // Infirmier
	"D307": BucketPharmacie,          // Pharmacie
	// Petite enfance
	"D502": BucketCreche, // Établissement d'accueil du jeune enfant
	"D504": BucketCreche, // Relais petite enfance
	// Transport
	"E107": BucketGare, // Gare nationale
	"E108": BucketGare, // Gare régionale
	"E109": BucketGare, // Gare locale
	// Sport
	"F121": BucketSportSalle,   // Salles multisports / gymnases
	"F101": BucketSportPiscine, // Bassin de natation
	"F107": BucketSportTerrain, // Terrain de tennis
}

// Upstream column headers in DS_BPE_2024_data.csv.
const (
	colGEO       = "GEO"           // commune/dep/region/... code
	colGEOObject = "GEO_OBJECT"    // geographic level (we keep "COM")
	colType      = "FACILITY_TYPE" // TYPEQU code
	colValue     = "OBS_VALUE"     // equipment count
)

// geoObjectCommune is the GEO_OBJECT value selecting commune-level rows.
// INSEE publishes the same counts at many levels (DEP, REG, EPCI, ARM…);
// filtering to COM yields one bucket vector per commune and naturally
// folds Paris/Lyon/Marseille to their parent communes (75056/69123/13055),
// since the per-arrondissement rows are GEO_OBJECT=ARM, not COM.
const geoObjectCommune = "COM"

// transform rebuilds the processed bpe artifact from the upstream INSEE ZIP.
// It reads DS_BPE_2024_data.csv from the archive, keeps GEO_OBJECT=COM rows
// whose FACILITY_TYPE is in the curated bucket map, sums OBS_VALUE per
// (commune, bucket), and writes the gzipped Index to dst.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	// archive/zip needs a ReaderAt + size; buffer the (modest, ~13 MB) ZIP.
	zbuf, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("bpe: read raw zip: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(zbuf), int64(len(zbuf)))
	if err != nil {
		return fmt.Errorf("bpe: open zip: %w", err)
	}

	var member io.ReadCloser
	for _, f := range zr.File {
		if f.Name == csvMemberName {
			member, err = f.Open()
			if err != nil {
				return fmt.Errorf("bpe: open %s: %w", csvMemberName, err)
			}
			break
		}
	}
	if member == nil {
		return fmt.Errorf("bpe: %s not found in zip", csvMemberName)
	}
	defer func() { _ = member.Close() }()

	idx, err := buildIndex(member)
	if err != nil {
		return err
	}

	if err := dataset.WriteGzJSON(dst, idx); err != nil {
		return fmt.Errorf("bpe: encode json: %w", err)
	}
	return nil
}

// buildIndex parses the (already-decompressed) BPE data CSV and aggregates
// it into the curated per-commune Index.
func buildIndex(r io.Reader) (*Index, error) {
	cr := csv.NewReader(dataset.BOMReader(r))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	cr.ReuseRecord = true

	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("bpe: read header: %w", err)
	}
	geo := indexOf(header, colGEO)
	gobj := indexOf(header, colGEOObject)
	ftype := indexOf(header, colType)
	val := indexOf(header, colValue)
	if geo < 0 || gobj < 0 || ftype < 0 || val < 0 {
		return nil, fmt.Errorf("bpe: header missing required columns: %v", header)
	}

	communes := map[string]map[Bucket]int{}
	bucketTotals := map[string]int{}
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("bpe: read row: %w", err)
		}
		if strings.TrimSpace(rec[gobj]) != geoObjectCommune {
			continue
		}
		bucket, ok := bucketByFacilityType[strings.TrimSpace(rec[ftype])]
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(rec[val]))
		if err != nil || n <= 0 {
			continue
		}
		insee := strings.TrimSpace(rec[geo])
		if insee == "" {
			continue
		}
		m := communes[insee]
		if m == nil {
			m = map[Bucket]int{}
			communes[insee] = m
		}
		m[bucket] += n
		bucketTotals[string(bucket)] += n
	}
	if len(communes) == 0 {
		return nil, errors.New("bpe: transform produced no communes")
	}

	return &Index{
		Meta: Meta{
			Source:           metaSource,
			ReferenceDate:    referenceDate,
			RowCountCommunes: len(communes),
			BucketTotals:     bucketTotals,
			Note:             metaNote,
		},
		Communes: communes,
	}, nil
}

// validate gates publication: the rebuilt artifact must parse and be
// non-empty.
func validate(r io.Reader) error {
	idx, err := parseIndex(r)
	if err != nil {
		return err
	}
	if idx.Count() == 0 {
		return errors.New("bpe: validated artifact has no communes")
	}
	return nil
}

// indexOf returns the index of the column whose trimmed header equals name,
// or -1.
func indexOf(header []string, name string) int {
	for i, h := range header {
		if strings.TrimSpace(h) == name {
			return i
		}
	}
	return -1
}
