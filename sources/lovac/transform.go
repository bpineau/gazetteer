package lovac

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

// rawCSVName is the datadir filename for the upstream raw input.
const rawCSVName = "lovac.raw.csv"

// rawCSVURL is the LOVAC "Logements vacants du parc privé, Communes" CSV on
// data.gouv.fr (dataset slug logements-vacants-du-parc-prive-par-commune-
// departement-region-france-de-2020-a-2026), addressed by its stable
// resource id: data.gouv.fr redirects it to the current dated upload, so a
// re-upload of the communal file is picked up without touching this URL.
// The yearly edition renames the columns (CODGEO_26, pp_vacant_26, ...);
// lovacColumns follows the newest year present, so a new edition needs no
// code change either, unless the producer publishes it as a new resource.
const rawCSVURL = "https://www.data.gouv.fr/fr/datasets/r/2e0417b4-902d-4c60-90e7-bf5df148cb87"

// Upstream column-name prefixes, each followed by a two-digit edition year
// (e.g. "pp_vacant_26" is the LOVAC 2026 edition). An edition YY counts the
// vacant private dwellings of edition YY but the total private park only
// from the land files of edition YY-1, so the headline rate is
// pp_vacant_YY / ff_pp_total_(YY-1). Editions up to 2025 named the total
// "pp_total_" (no "ff_" prefix).
const (
	colINSEEPrefix      = "CODGEO_"
	colVacantPrefix     = "pp_vacant_"
	colVacantLongPrefix = "pp_vacant_plus_2ans_"
)

// colTotalPrefixes are the spellings of the total private park column,
// newest first.
var colTotalPrefixes = []string{"ff_pp_total_", "pp_total_"}

// Processed column names. They are deliberately free of the edition year,
// which moves every year; legacyRateCol / legacyLongCol are the names an
// artifact built from the 2025 edition carried, still accepted on read.
const (
	colOutINSEE   = "INSEE_C"
	colOutRate    = "taux_vacance_pct"
	colOutLong    = "taux_vacance_long_pct"
	legacyRateCol = "taux_vacance_25_pct"
	legacyLongCol = "taux_vacance_long_25_pct"
)

// outHeader is the compact schema the source parses (see loader.go).
var outHeader = []string{colOutINSEE, colOutRate, colOutLong}

// columns locates, in one LOVAC header, the columns of its newest edition.
type columns struct {
	year                          int // two-digit edition year, e.g. 26
	insee, vacant, vacLong, total int
}

// lovacColumns picks the newest edition year YY for which the header
// carries pp_vacant_YY, then resolves that edition's INSEE, vacant,
// long-term vacant and total columns (the total being the YY-1 land-file
// count). The choice is deterministic: the highest year wins, whatever the
// column order.
func lovacColumns(header []string) (columns, error) {
	year := -1
	for _, h := range header {
		// "pp_vacant_plus_2ans_YY" shares the prefix but not the shape, so
		// yearSuffix rejects it.
		if y, ok := yearSuffix(strings.TrimSpace(h), colVacantPrefix); ok && y > year {
			year = y
		}
	}
	if year < 1 {
		return columns{}, fmt.Errorf("lovac: no %sYY column in header %v", colVacantPrefix, header)
	}
	c := columns{
		year:    year,
		insee:   indexOf(header, fmt.Sprintf("%s%02d", colINSEEPrefix, year)),
		vacant:  indexOf(header, fmt.Sprintf("%s%02d", colVacantPrefix, year)),
		vacLong: indexOf(header, fmt.Sprintf("%s%02d", colVacantLongPrefix, year)),
		total:   -1,
	}
	for _, p := range colTotalPrefixes {
		if i := indexOf(header, fmt.Sprintf("%s%02d", p, year-1)); i >= 0 {
			c.total = i
			break
		}
	}
	if c.insee < 0 || c.vacant < 0 || c.vacLong < 0 || c.total < 0 {
		return columns{}, fmt.Errorf("lovac: edition %02d is missing required columns: %v", year, header)
	}
	return c, nil
}

// yearSuffix reports the two-digit year that follows prefix in name
// ("pp_vacant_26" → 26), and false when name is not exactly prefix + 2
// digits.
func yearSuffix(name, prefix string) (int, bool) {
	rest, ok := strings.CutPrefix(name, prefix)
	if !ok || len(rest) != 2 {
		return 0, false
	}
	y, err := strconv.Atoi(rest)
	if err != nil {
		return 0, false
	}
	return y, true
}

// transform rebuilds the processed vacance CSV from the upstream LOVAC
// communal file, reading its newest edition (see lovacColumns). For every
// commune whose total private park is a positive number it emits the
// headline and long-term vacancy rates (count / total × 100); suppressed
// counts ("s", for fewer than 11 dwellings) yield an empty rate cell, but
// the commune row is still written.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawCSVName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	cr := csv.NewReader(rc)
	cr.Comma = ';'
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("vacance: read header: %w", err)
	}
	cols, err := lovacColumns(header)
	if err != nil {
		return err
	}
	iInsee, iVac, iVacLong, iTotal := cols.insee, cols.vacant, cols.vacLong, cols.total

	w := csv.NewWriter(dst)
	w.Comma = ';'
	if err := w.Write(outHeader); err != nil {
		return err
	}
	n := 0
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("vacance: read row: %w", err)
		}
		insee := strings.TrimSpace(rec[iInsee])
		if insee == "" {
			continue
		}
		total, ok := parseCount(rec[iTotal])
		if !ok || total <= 0 {
			continue // no usable denominator → commune excluded
		}
		if err := w.Write([]string{
			insee,
			rate(rec[iVac], total),
			rate(rec[iVacLong], total),
		}); err != nil {
			return err
		}
		n++
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	if n == 0 {
		return errors.New("vacance: transform produced no rows")
	}
	return nil
}

// validate gates publication: the rebuilt CSV must parse and be non-empty.
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

// rate renders count/total × 100 as a percentage with the same formatting as
// the published artifact: rounded half-to-even to two decimals, then trimmed
// to the shortest form that keeps at least one decimal (e.g. "6.29", "9.5",
// "0.0"). A suppressed or empty count yields "".
func rate(countCell string, total float64) string {
	count, ok := parseCount(countCell)
	if !ok {
		return ""
	}
	// strconv 'f' with precision 2 rounds half-to-even on the exact float,
	// matching the Python round(…, 2) used to build the committed file.
	s := strconv.FormatFloat(100*count/total, 'f', 2, 64)
	s = strings.TrimRight(s, "0")
	if strings.HasSuffix(s, ".") {
		s += "0"
	}
	return s
}

// parseCount parses an integer-ish count. ok is false for empty cells and
// the LOVAC "s" statistical-secrecy marker (and any other non-numeric).
func parseCount(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "s" || s == "NA" || s == "ND" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
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
