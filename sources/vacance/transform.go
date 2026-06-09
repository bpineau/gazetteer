package vacance

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

// rawName is the datadir filename for the upstream raw input. INSEE ships
// the "base communale logement" as a zip; the Transform unzips it in
// memory and reads the data CSV member (see transform).
const rawName = "vacance.raw.zip"

// rawURL is INSEE's "base-cc-logement-2021_csv.zip" (Recensement de la
// Population 2021 — base communale logement, file 8202349 on insee.fr).
// The zip carries two members: the per-commune data CSV
// (base-cc-logement-2021.CSV) and a meta_*.CSV variable dictionary. Bump
// this URL — and dataYear / the P21_ column constants — when INSEE
// publishes a fresh census vintage.
const rawURL = "https://www.insee.fr/fr/statistiques/fichier/8202349/base-cc-logement-2021_csv.zip"

// metaSource is the provenance string recorded in the rebuilt artifact. It
// is kept byte-identical to the committed embedded blob.
const metaSource = "INSEE Recensement de la Population 2021 — base communale logement (file 8202349)"

// metaNote documents the derivation, recorded into the rebuilt artifact.
// (The committed blob predates the gazetteer/lovac rename and still says
// "gazetteer/vacance" here; a `refresh --go-embed-update vacance` rebuild
// propagates this corrected text — it is provenance only, read by nothing
// at query time.)
const metaNote = "Demographic vacancy rate = P21_LOGVAC / P21_LOG. Distinct from the " +
	"gazetteer/lovac source, which carries the LOVAC fiscal status (Taxe " +
	"sur les Logements Vacants 2013). Paris/Lyon/Marseille publish per-" +
	"arrondissement rows in this dataset."

// dataYear is the census vintage of the upstream resource. The CSV does not
// carry it inline; keep it in sync with rawURL / the P21_ columns.
const dataYear = 2021

// Upstream column headers (INSEE RP 2021 base logement). Values are
// statistically-weighted floats; the Transform rounds the counts to the
// nearest integer (matching the published artifact).
const (
	colINSEE = "CODGEO"
	colLog   = "P21_LOG"     // total logements
	colVac   = "P21_LOGVAC"  // logements vacants
	colRP    = "P21_RP"      // résidences principales
	colRSec  = "P21_RSECOCC" // résidences secondaires + logements occasionnels
)

// metaCSVPrefix marks the variable-dictionary member of the upstream zip,
// which must be skipped in favour of the per-commune data CSV.
const metaCSVPrefix = "meta_"

// transform rebuilds the processed vacance artifact from the
// upstream INSEE zip. It locates the data CSV member (the non-"meta_" .CSV),
// keeps every 5-digit-INSEE row with a positive total logement count,
// rounds the weighted counts to integers and derives the vacancy rate
// (P21_LOGVAC / P21_LOG, percent, two decimals). Output is gzipped JSON, as
// the committed artifact is .json.gz.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	blob, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("vacance: read raw zip: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		return fmt.Errorf("vacance: open raw zip: %w", err)
	}
	member, err := dataCSVMember(zr)
	if err != nil {
		return err
	}
	mrc, err := member.Open()
	if err != nil {
		return fmt.Errorf("vacance: open zip member %q: %w", member.Name, err)
	}
	defer func() { _ = mrc.Close() }()

	cr := csv.NewReader(dataset.BOMReader(mrc))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("vacance: read header: %w", err)
	}
	insee := indexOf(header, colINSEE)
	logC := indexOf(header, colLog)
	vacC := indexOf(header, colVac)
	rpC := indexOf(header, colRP)
	rsecC := indexOf(header, colRSec)
	if insee < 0 || logC < 0 || vacC < 0 || rpC < 0 || rsecC < 0 {
		return fmt.Errorf("vacance: header missing required columns: %v", header)
	}

	idx := Index{
		Meta: Meta{
			Source:   metaSource,
			DataYear: dataYear,
			Note:     metaNote,
		},
		Communes: map[string]Entry{},
	}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("vacance: read row: %w", err)
		}
		code := strings.TrimSpace(rec[insee])
		if len(code) != 5 {
			continue // national/department aggregates, or blanks
		}
		log, ok := parseCount(rec[logC])
		if !ok || log <= 0 {
			continue // suppressed or empty — cannot compute a rate
		}
		vac, ok := parseCount(rec[vacC])
		if !ok {
			continue
		}
		rp, ok := parseCount(rec[rpC])
		if !ok {
			continue
		}
		rsec, ok := parseCount(rec[rsecC])
		if !ok {
			continue
		}
		idx.Communes[code] = Entry{
			// Round half-to-even, matching Python's round() used to build
			// the committed blob (INSEE weighting yields .5 half-cases).
			Log:            int(math.RoundToEven(log)),
			Vac:            int(math.RoundToEven(vac)),
			RP:             int(math.RoundToEven(rp)),
			RSec:           int(math.RoundToEven(rsec)),
			VacancyRatePct: round2(vac / log * 100.0),
		}
	}
	if len(idx.Communes) == 0 {
		return errors.New("vacance: transform produced no communes")
	}
	idx.Meta.RowCountCommunes = len(idx.Communes)

	if err := dataset.WriteGzJSON(dst, idx); err != nil {
		return fmt.Errorf("vacance: encode json: %w", err)
	}
	return nil
}

// validate gates publication: the rebuilt (gzipped) artifact must gunzip,
// parse and be non-empty.
func validate(r io.Reader) error {
	idx, err := parseIndex(r)
	if err != nil {
		return err
	}
	if idx.Count() == 0 {
		return errors.New("vacance: validated artifact has no communes")
	}
	return nil
}

// dataCSVMember returns the per-commune data CSV of the upstream zip: the
// .CSV member whose basename does not carry the "meta_" variable-dictionary
// prefix.
func dataCSVMember(zr *zip.Reader) (*zip.File, error) {
	for _, f := range zr.File {
		base := f.Name
		if i := strings.LastIndexByte(base, '/'); i >= 0 {
			base = base[i+1:]
		}
		if !strings.EqualFold(strings.TrimSpace(base), "") &&
			strings.HasSuffix(strings.ToLower(base), ".csv") &&
			!strings.HasPrefix(strings.ToLower(base), metaCSVPrefix) {
			return f, nil
		}
	}
	return nil, errors.New("vacance: data CSV not found in raw zip")
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

// parseCount parses an INSEE weighted count cell (a plain float such as
// "372.387493855914"). ok is false for an empty/non-numeric cell.
func parseCount(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// round2 rounds to two decimals (the VacancyRatePct rule). It rounds
// half-to-even, matching Python's round(x, 2) used to build the committed
// blob (e.g. 3.125 → 3.12, not 3.13).
func round2(x float64) float64 {
	return math.RoundToEven(x*100) / 100
}
