package logiris

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/frnorm"
	"github.com/bpineau/gazetteer/helpers/stats"
)

// rawZipName is the datadir filename for the upstream raw input.
const rawZipName = "logiris.raw.zip"

// rawZipURL is INSEE's "Logement en 2021 (IRIS)" base — base-ic-logement,
// CSV variant. Bump this (and dataYear) on a new census vintage.
const rawZipURL = "https://www.insee.fr/fr/statistiques/fichier/8268838/base-ic-logement-2021_csv.zip"

const metaSource = "insee.fr/fr/statistiques/8268838 — base-ic-logement-2021 (RP 2021, IRIS)"

const metaNote = "renter_share_pct = P21_RP_LOC / P21_RP. social_housing_share_pct = " +
	"P21_RP_LOCHLMV / P21_RP. vacancy_rate_pct = P21_LOGVAC / P21_LOG. Île-de-France IRIS " +
	"only (matches the iris resolver scope); rows with no résidences principales / no dwellings, " +
	"or fewer than minDwellings dwellings (statistically thin, suppression-prone), are dropped."

// minDwellings is the floor below which an IRIS is dropped: INSEE suppresses
// small counts (a blank cell reads as a misleading 0 share) and IRIS-level
// ratios on a handful of dwellings are noise. 50 clears the bulk of the
// secret-statistique rows while keeping every real residential IRIS.
const minDwellings = 50

// dataYear is the census vintage of the upstream resource.
const dataYear = 2021

// Upstream column headers (RP 2021 logement block).
const (
	colIRIS = "IRIS"
	colLog  = "P21_LOG"
	colRP   = "P21_RP"
	colLoc  = "P21_RP_LOC"
	colHLM  = "P21_RP_LOCHLMV"
	colVac  = "P21_LOGVAC"
)

// metaCSVPrefix marks the metadata CSV member inside the INSEE zip.
const metaCSVPrefix = "meta_"

// idfDepts are the Île-de-France department codes (the first two chars of an
// IRIS code). The embedded artifact is scoped to these, matching the IRIS
// resolver's coverage and keeping the processed JSON well under the embed
// budget (the national base is ~49 000 IRIS / ~3.7 MB).
var idfDepts = map[string]struct{}{
	"75": {}, "77": {}, "78": {}, "91": {}, "92": {}, "93": {}, "94": {}, "95": {},
}

// transform rebuilds the processed logiris artifact from the upstream INSEE
// zip: it locates the data CSV member, keeps every Île-de-France 9-char IRIS
// row carrying résidences principales, derives the renter / social-housing /
// vacancy shares, and writes gzipped JSON.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawZipName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	blob, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("logiris: read raw zip: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		return fmt.Errorf("logiris: open raw zip: %w", err)
	}
	member, err := dataCSVMember(zr)
	if err != nil {
		return err
	}
	mrc, err := member.Open()
	if err != nil {
		return fmt.Errorf("logiris: open zip member %q: %w", member.Name, err)
	}
	defer func() { _ = mrc.Close() }()

	cr := csv.NewReader(dataset.BOMReader(mrc))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("logiris: read header: %w", err)
	}
	iris, logc, rp := indexOf(header, colIRIS), indexOf(header, colLog), indexOf(header, colRP)
	loc, hlm, vac := indexOf(header, colLoc), indexOf(header, colHLM), indexOf(header, colVac)
	if iris < 0 || logc < 0 || rp < 0 || loc < 0 || vac < 0 {
		return fmt.Errorf("logiris: header missing required columns: %v", header)
	}

	idx := Index{
		Meta: Meta{
			Source:       metaSource,
			DataYear:     dataYear,
			Scope:        "Île-de-France",
			Note:         metaNote,
			DownloadedAt: time.Now().UTC().Format("2006-01-02"),
		},
		IRIS: map[string]Entry{},
	}
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("logiris: read row: %w", err)
		}
		code := strings.TrimSpace(at(rec, iris))
		if len(code) != 9 {
			continue // aggregate rows / blanks
		}
		if _, ok := idfDepts[code[:2]]; !ok {
			continue // outside Île-de-France
		}
		logTot, ok1 := frnorm.ParseFRFloat(at(rec, logc))
		rpTot, ok2 := frnorm.ParseFRFloat(at(rec, rp))
		if !ok1 || !ok2 || rpTot <= 0 || logTot < minDwellings {
			continue // no résidences principales, or too few dwellings to trust
		}
		e := Entry{
			TotalLogements: int(math.RoundToEven(logTot)),
			RenterSharePct: share(at(rec, loc), rpTot),
			VacancyRatePct: share(at(rec, vac), logTot),
		}
		if hlm >= 0 {
			e.SocialHousingSharePct = share(at(rec, hlm), rpTot)
		}
		idx.IRIS[code] = e
	}
	if len(idx.IRIS) == 0 {
		return errors.New("logiris: transform produced no IRIS rows")
	}
	idx.Meta.RowCountIRIS = len(idx.IRIS)

	if err := dataset.WriteGzJSON(dst, idx); err != nil {
		return fmt.Errorf("logiris: encode json: %w", err)
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
		return errors.New("logiris: validated artifact has no IRIS rows")
	}
	return nil
}

// share returns 100 * numerator/denominator rounded to one decimal, or 0
// when the numerator cell is empty/suppressed. denominator is assumed > 0.
func share(numCell string, denominator float64) float64 {
	num, ok := frnorm.ParseFRFloat(numCell)
	if !ok {
		return 0
	}
	return stats.Round(num/denominator*100.0, 1)
}

// dataCSVMember returns the single data CSV member of the INSEE zip — the
// .csv whose name does not start with the "meta_" prefix.
func dataCSVMember(zr *zip.Reader) (*zip.File, error) {
	for _, f := range zr.File {
		name := f.Name
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		lname := strings.ToLower(name)
		if strings.HasPrefix(lname, metaCSVPrefix) {
			continue
		}
		if strings.HasSuffix(lname, ".csv") {
			return f, nil
		}
	}
	return nil, errors.New("logiris: no data CSV member in zip")
}

// at returns the column at idx, or "" when idx is out of range / negative.
func at(rec []string, idx int) string {
	if idx < 0 || idx >= len(rec) {
		return ""
	}
	return rec[idx]
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
