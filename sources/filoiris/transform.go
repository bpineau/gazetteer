package filoiris

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
const rawZipName = "filoiris.raw.zip"

// rawZipURL is INSEE's "Revenus, pauvreté et niveau de vie en 2021 (Iris)"
// disposable-income base, CSV variant. Bump this (and dataYear) when INSEE
// publishes a new Filosofi IRIS vintage.
const rawZipURL = "https://www.insee.fr/fr/statistiques/fichier/8229323/BASE_TD_FILO_IRIS_2021_DISP_CSV.zip"

const metaSource = "insee.fr/fr/statistiques/8229323 — BASE_TD_FILO_IRIS_2021_DISP (Filosofi 2021, IRIS)"

const metaNote = "median_eur = revenu disponible médian annuel par UC (€) at IRIS level. " +
	"poverty_rate_pct = taux de pauvreté au seuil de 60 % (DISP_TP60). gini = Gini index of " +
	"disposable income. national_median_eur = median of IRIS medians. Coverage: IRIS of communes " +
	"≥ 5000 inhabitants; suppressed cells (ns/nd) are dropped."

// dataYear is the Filosofi vintage of the upstream resource.
const dataYear = 2021

// Upstream column headers (Filosofi "disponible" — DISP — block, 2021 suffix).
const (
	colIRIS    = "IRIS"
	colMedian  = "DISP_MED21"
	colPoverty = "DISP_TP6021"
	colGini    = "DISP_GI21"
)

// metaCSVPrefix marks the metadata CSV member inside the INSEE zip (the
// data member is the other .csv).
const metaCSVPrefix = "meta_"

// transform rebuilds the processed filoiris artifact from the upstream INSEE
// zip. It locates the data CSV member, keeps every 9-char IRIS row carrying
// a published median disposable income (suppressed cells — "ns"/"nd" — are
// dropped), records the optional poverty rate + Gini, derives the national
// figure as the median of IRIS medians, and writes gzipped JSON.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawZipName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	blob, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("filoiris: read raw zip: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		return fmt.Errorf("filoiris: open raw zip: %w", err)
	}
	member, err := dataCSVMember(zr)
	if err != nil {
		return err
	}
	mrc, err := member.Open()
	if err != nil {
		return fmt.Errorf("filoiris: open zip member %q: %w", member.Name, err)
	}
	defer func() { _ = mrc.Close() }()

	cr := csv.NewReader(dataset.BOMReader(mrc))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("filoiris: read header: %w", err)
	}
	iris, median := indexOf(header, colIRIS), indexOf(header, colMedian)
	poverty, gini := indexOf(header, colPoverty), indexOf(header, colGini)
	if iris < 0 || median < 0 {
		return fmt.Errorf("filoiris: header missing %q/%q: %v", colIRIS, colMedian, header)
	}

	idx := Index{
		Meta: Meta{
			Source:       metaSource,
			DataYear:     dataYear,
			Note:         metaNote,
			DownloadedAt: time.Now().UTC().Format("2006-01-02"),
		},
		IRIS: map[string]Entry{},
	}
	var medians []int
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("filoiris: read row: %w", err)
		}
		code := strings.TrimSpace(rec[iris])
		if len(code) != 9 {
			continue // aggregate rows / blanks
		}
		med, ok := parseEuro(at(rec, median))
		if !ok {
			continue // suppressed (ns/nd) or empty
		}
		e := Entry{MedianEUR: med}
		if pct, ok := frnorm.ParseFRFloat(at(rec, poverty)); ok {
			e.PovertyRatePct = pct
		}
		if g, ok := frnorm.ParseFRFloat(at(rec, gini)); ok {
			e.Gini = g
		}
		idx.IRIS[code] = e
		medians = append(medians, med)
	}
	if len(idx.IRIS) == 0 {
		return errors.New("filoiris: transform produced no IRIS rows")
	}
	idx.Meta.RowCountIRIS = len(idx.IRIS)
	idx.Meta.NationalMedianEUR = stats.MedianInt(medians)

	if err := dataset.WriteGzJSON(dst, idx); err != nil {
		return fmt.Errorf("filoiris: encode json: %w", err)
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
		return errors.New("filoiris: validated artifact has no IRIS rows")
	}
	return nil
}

// dataCSVMember returns the single data CSV member of the INSEE zip — the
// .csv whose name does not start with the "meta_" prefix.
func dataCSVMember(zr *zip.Reader) (*zip.File, error) {
	for _, f := range zr.File {
		name := f.Name
		if i := strings.LastIndex(name, "/"); i >= 0 {
			name = name[i+1:]
		}
		// Case-insensitive on both checks: INSEE has shipped mixed casing.
		lname := strings.ToLower(name)
		if strings.HasPrefix(lname, metaCSVPrefix) {
			continue
		}
		if strings.HasSuffix(lname, ".csv") {
			return f, nil
		}
	}
	return nil, errors.New("filoiris: no data CSV member in zip")
}

// at returns the column at idx, or "" when idx is out of range / negative
// (an optional column absent from the header).
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

// parseEuro parses a euro amount into a rounded integer. ok is false for
// empty / suppressed ("ns", "nd") cells.
func parseEuro(s string) (int, bool) {
	f, ok := frnorm.ParseFRFloat(s)
	if !ok {
		return 0, false
	}
	return int(math.Round(f)), true
}
