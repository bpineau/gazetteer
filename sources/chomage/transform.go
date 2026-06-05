package chomage

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"

	"github.com/bpineau/gazetteer/dataset"
)

// ratesRawName / appartRawName are the datadir filenames for the two
// upstream raw inputs (both INSEE Excel artifacts; the appartenance table
// ships inside a ZIP).
const (
	ratesRawName  = "chomage_zones_emploi.raw.xlsx"
	appartRawName = "table_appartenance_communes.raw.zip"
)

// ratesURL is the INSEE "Taux de chômage localisés par zone d'emploi"
// quarterly series (ZE2020), an xlsx covering 2003-T1 onward. appartURL is
// the INSEE "Table d'appartenance géographique des communes" ZIP, whose
// xlsx member carries the commune → ZE2020 crosswalk and the ZE labels.
// Bump both — together with latestQuarter below — when INSEE publishes a
// newer edition.
const (
	ratesURL  = "https://www.insee.fr/fr/statistiques/fichier/1893230/chomage-zone-t1-2003-t4-2025.xlsx"
	appartURL = "https://www.insee.fr/fr/statistiques/fichier/7671844/table-appartenance-geo-communes-2025.zip"
)

// Sheet names inside the two workbooks.
const (
	ratesSheet = "txcho_ze"
	comSheet   = "COM"
	supraSheet = "Zones_supra_communales"
	supraNivZE = "ZE2020"
)

// Column headers located within each sheet's machine-header row (the INSEE
// sheets carry a few banner/title rows before it; the header row is found
// by locating the cell that equals colCODGEO / colZE2020).
const (
	colZE2020 = "ZE2020"
	colCODGEO = "CODGEO"
	colNIVGEO = "NIVGEO"
	colLIBGEO = "LIBGEO"
)

// keptQuarters is the number of trailing quarters retained from the rates
// series (oldest-first). Pinned, delinquance-style; latestQuarter records
// the most recent quarter of the published edition for provenance. Bump
// both — with the URLs above — on a refresh.
const (
	keptQuarters  = 20
	latestQuarter = "2025-T4"
)

// metaSource / metaNote mirror the committed artifact's meta.
const (
	metaSource = "INSEE Estimations de taux de chômage localisés (ZE2020) + table d appartenance des communes"
	metaNote   = "Quarterly seasonally-adjusted unemployment rate per zone d emploi 2020. Mayotte + Guyane excluded by source. Last 20 quarters retained for trend."
)

// transform rebuilds the processed chômage artifact from the two INSEE
// inputs: the per-ZE quarterly unemployment-rate xlsx and the commune
// appartenance ZIP (a ZIP wrapping an xlsx).
//
//   - From the rates sheet it keeps each ZE's last keptQuarters rates,
//     oldest-first, aligned with the Quarters slice.
//   - From the appartenance COM sheet it builds the commune → ZE2020
//     crosswalk; from the supra-communal sheet (NIVGEO == ZE2020) it takes
//     the canonical ZE labels (these carry the accented forms, e.g.
//     "Évry-Courcouronnes", which the rates file does not).
//   - The national series is the per-quarter unweighted mean across ZEs,
//     rounded to 2 dp; national_rate_pct is its last value.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	quarters, zes, err := readRates(raw)
	if err != nil {
		return err
	}
	communes, labels, err := readAppartenance(raw)
	if err != nil {
		return err
	}

	zones := make(map[string]ZoneEntry, len(zes))
	for ze, series := range zes {
		zones[ze] = ZoneEntry{Label: labels[ze], RatePct: series}
	}

	natSeries := nationalSeries(zes, len(quarters))
	national := 0.0
	if len(natSeries) > 0 {
		national = natSeries[len(natSeries)-1]
	}

	idx := Index{
		Meta: Meta{
			Source:          metaSource,
			SeriesStart:     quarters[0],
			SeriesEnd:       quarters[len(quarters)-1],
			QuarterCount:    len(quarters),
			ZECount:         len(zones),
			CommuneCount:    len(communes),
			NationalRatePct: national,
			Note:            metaNote,
		},
		Quarters:              quarters,
		NationalRatePctSeries: natSeries,
		Zones:                 zones,
		Communes:              communes,
	}

	if idx.Meta.ZECount == 0 || idx.Meta.CommuneCount == 0 {
		return errors.New("chomage: transform produced no zones or communes")
	}

	enc := json.NewEncoder(dst)
	return enc.Encode(idx)
}

// readRates opens the rates xlsx and returns the kept quarter labels
// (oldest-first) and each ZE's aligned rate series.
func readRates(raw dataset.RawSet) ([]string, map[string][]float64, error) {
	body, err := readAll(raw, ratesRawName)
	if err != nil {
		return nil, nil, err
	}
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		return nil, nil, fmt.Errorf("chomage: open rates xlsx: %w", err)
	}
	defer func() { _ = f.Close() }()

	rows, err := f.GetRows(ratesSheet)
	if err != nil {
		return nil, nil, fmt.Errorf("chomage: read sheet %q: %w", ratesSheet, err)
	}
	h := headerRow(rows, colZE2020)
	if h < 0 {
		return nil, nil, fmt.Errorf("chomage: %q header (%s) not found", ratesSheet, colZE2020)
	}
	hdr := rows[h]
	zeCol := columnIndex(hdr, colZE2020)

	// Quarter columns are the headers shaped like "2003-T1".
	var qCols []int
	var allQuarters []string
	for i, c := range hdr {
		if isQuarter(strings.TrimSpace(c)) {
			qCols = append(qCols, i)
			allQuarters = append(allQuarters, strings.TrimSpace(c))
		}
	}
	if len(qCols) < keptQuarters {
		return nil, nil, fmt.Errorf("chomage: only %d quarter columns, need %d", len(qCols), keptQuarters)
	}
	start := len(qCols) - keptQuarters
	keptCols := qCols[start:]
	quarters := append([]string(nil), allQuarters[start:]...)
	if quarters[len(quarters)-1] != latestQuarter {
		return nil, nil, fmt.Errorf("chomage: latest quarter %q != pinned %q (edition drift — bump the URLs and latestQuarter)", quarters[len(quarters)-1], latestQuarter)
	}

	zes := make(map[string][]float64)
	for _, r := range rows[h+1:] {
		if zeCol >= len(r) {
			continue
		}
		ze := strings.TrimSpace(r[zeCol])
		if ze == "" {
			continue
		}
		series := make([]float64, len(keptCols))
		for j, ci := range keptCols {
			series[j] = parseRate(cell(r, ci))
		}
		zes[ze] = series
	}
	if len(zes) == 0 {
		return nil, nil, errors.New("chomage: no ZE rows in rates sheet")
	}
	return quarters, zes, nil
}

// readAppartenance opens the appartenance ZIP, locates its xlsx member, and
// returns the commune → ZE2020 crosswalk and the ZE2020 → label map.
func readAppartenance(raw dataset.RawSet) (map[string]string, map[string]string, error) {
	body, err := readAll(raw, appartRawName)
	if err != nil {
		return nil, nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, nil, fmt.Errorf("chomage: open appartenance zip: %w", err)
	}
	var member *zip.File
	for _, zf := range zr.File {
		if strings.HasSuffix(strings.ToLower(zf.Name), ".xlsx") {
			member = zf
			break
		}
	}
	if member == nil {
		return nil, nil, errors.New("chomage: no xlsx member in appartenance zip")
	}
	rc, err := member.Open()
	if err != nil {
		return nil, nil, fmt.Errorf("chomage: open zip member %q: %w", member.Name, err)
	}
	xbytes, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return nil, nil, fmt.Errorf("chomage: read zip member %q: %w", member.Name, err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(xbytes))
	if err != nil {
		return nil, nil, fmt.Errorf("chomage: open appartenance xlsx: %w", err)
	}
	defer func() { _ = f.Close() }()

	communes, err := readCommunes(f)
	if err != nil {
		return nil, nil, err
	}
	labels, err := readLabels(f)
	if err != nil {
		return nil, nil, err
	}
	return communes, labels, nil
}

// readCommunes builds the commune INSEE → ZE2020 crosswalk from the COM
// sheet.
func readCommunes(f *excelize.File) (map[string]string, error) {
	rows, err := f.GetRows(comSheet)
	if err != nil {
		return nil, fmt.Errorf("chomage: read sheet %q: %w", comSheet, err)
	}
	h := headerRow(rows, colCODGEO)
	if h < 0 {
		return nil, fmt.Errorf("chomage: %q header (%s) not found", comSheet, colCODGEO)
	}
	codgeo := columnIndex(rows[h], colCODGEO)
	ze := columnIndex(rows[h], colZE2020)
	if codgeo < 0 || ze < 0 {
		return nil, fmt.Errorf("chomage: %q missing CODGEO/ZE2020 columns", comSheet)
	}
	communes := make(map[string]string)
	for _, r := range rows[h+1:] {
		c := strings.TrimSpace(cell(r, codgeo))
		z := strings.TrimSpace(cell(r, ze))
		if c == "" || z == "" {
			continue
		}
		communes[c] = z
	}
	if len(communes) == 0 {
		return nil, errors.New("chomage: no commune rows in COM sheet")
	}
	return communes, nil
}

// readLabels builds the ZE2020 → label map from the supra-communal sheet,
// keeping only rows whose NIVGEO is ZE2020. These labels carry the
// canonical accented forms used in the committed artifact.
func readLabels(f *excelize.File) (map[string]string, error) {
	rows, err := f.GetRows(supraSheet)
	if err != nil {
		return nil, fmt.Errorf("chomage: read sheet %q: %w", supraSheet, err)
	}
	h := headerRow(rows, colCODGEO)
	if h < 0 {
		return nil, fmt.Errorf("chomage: %q header (%s) not found", supraSheet, colCODGEO)
	}
	niv := columnIndex(rows[h], colNIVGEO)
	codgeo := columnIndex(rows[h], colCODGEO)
	lib := columnIndex(rows[h], colLIBGEO)
	if niv < 0 || codgeo < 0 || lib < 0 {
		return nil, fmt.Errorf("chomage: %q missing NIVGEO/CODGEO/LIBGEO columns", supraSheet)
	}
	labels := make(map[string]string)
	for _, r := range rows[h+1:] {
		if strings.TrimSpace(cell(r, niv)) != supraNivZE {
			continue
		}
		code := strings.TrimSpace(cell(r, codgeo))
		if code == "" {
			continue
		}
		labels[code] = strings.TrimSpace(cell(r, lib))
	}
	if len(labels) == 0 {
		return nil, errors.New("chomage: no ZE2020 rows in supra-communal sheet")
	}
	return labels, nil
}

// nationalSeries returns the per-quarter unweighted mean across all ZE
// series, rounded to 2 dp.
func nationalSeries(zes map[string][]float64, n int) []float64 {
	if n == 0 || len(zes) == 0 {
		return nil
	}
	out := make([]float64, n)
	for q := 0; q < n; q++ {
		sum := 0.0
		count := 0
		for _, s := range zes {
			// Skip blank/garbled cells: parseRate maps them to 0, and no
			// employment zone has a real 0 % unemployment rate, so a 0 means
			// "missing". Averaging it in would drag the national mean down.
			if q < len(s) && s[q] > 0 {
				sum += s[q]
				count++
			}
		}
		if count > 0 {
			out[q] = round2(sum / float64(count))
		}
	}
	return out
}

// validate gates publication: the rebuilt artifact must parse and carry
// both zones and communes.
func validate(r io.Reader) error {
	idx, err := parseIndex(r)
	if err != nil {
		return err
	}
	if idx.ZoneCount() == 0 {
		return errors.New("chomage: validated artifact has no zones")
	}
	if idx.CommuneCount() == 0 {
		return errors.New("chomage: validated artifact has no communes")
	}
	return nil
}

// readAll reads a named raw input fully into memory. The xlsx/zip readers
// both need random access, so streaming is not an option.
func readAll(raw dataset.RawSet, name string) ([]byte, error) {
	rc, err := raw.Open(name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	body, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("chomage: read %s: %w", name, err)
	}
	return body, nil
}

// headerRow returns the index of the first row containing a cell equal
// (case-insensitively, trimmed) to want, or -1.
func headerRow(rows [][]string, want string) int {
	for i, r := range rows {
		for _, c := range r {
			if strings.EqualFold(strings.TrimSpace(c), want) {
				return i
			}
		}
	}
	return -1
}

// columnIndex returns the index of the header cell equal to name, or -1.
func columnIndex(hdr []string, name string) int {
	for i, c := range hdr {
		if strings.EqualFold(strings.TrimSpace(c), name) {
			return i
		}
	}
	return -1
}

// cell returns row[i] or "" when the row is short (excelize trims trailing
// empties).
func cell(row []string, i int) string {
	if i < 0 || i >= len(row) {
		return ""
	}
	return row[i]
}

// isQuarter reports whether s looks like an INSEE quarter label "YYYY-T#".
func isQuarter(s string) bool {
	if len(s) != 7 || s[4] != '-' || s[5] != 'T' {
		return false
	}
	for _, r := range s[:4] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s[6] >= '1' && s[6] <= '4'
}

// parseRate parses a rate cell, tolerating a comma decimal separator and
// blanks (→ 0).
func parseRate(s string) float64 {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", "."))
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v
}

// round2 rounds to 2 decimal places.
func round2(x float64) float64 { return math.Round(x*100) / 100 }
