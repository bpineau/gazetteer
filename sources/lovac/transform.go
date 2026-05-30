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

// rawCSVURL is the LOVAC "Logements vacants du parc privé — Communes" CSV on
// data.gouv.fr (dataset slug logements-vacants-du-parc-prive-par-commune-
// departement-region-france). data.gouv mints a dated static path per
// vintage; bump this (and the year-suffixed column constants below) when a
// new LOVAC edition ships.
const rawCSVURL = "https://static.data.gouv.fr/resources/logements-vacants-du-parc-prive-lovac-par-commune-departement-region-et-france/20250528-090420/lovac-opendata-communes.csv"

// Upstream column names. LOVAC carries the vacancy stock at the current
// vintage (…_25) but the total private park only at the prior year (…_24);
// the headline rate is therefore vacant_25 / total_24.
const (
	colINSEE      = "CODGEO_25"
	colVacant     = "pp_vacant_25"
	colVacantLong = "pp_vacant_plus_2ans_25"
	colTotal      = "pp_total_24"
)

// outHeader is the compact schema the source parses (see loader.go).
var outHeader = []string{"INSEE_C", "taux_vacance_25_pct", "taux_vacance_long_25_pct"}

// transform rebuilds the processed vacance CSV from the upstream LOVAC
// communal file. For every commune whose total private park (pp_total_24) is
// a positive number it emits the headline and long-term vacancy rates
// (count / total × 100); suppressed counts ("s", for fewer than 11
// dwellings) yield an empty rate cell, but the commune row is still written.
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
	iInsee := indexOf(header, colINSEE)
	iVac := indexOf(header, colVacant)
	iVacLong := indexOf(header, colVacantLong)
	iTotal := indexOf(header, colTotal)
	if iInsee < 0 || iVac < 0 || iVacLong < 0 || iTotal < 0 {
		return fmt.Errorf("vacance: upstream missing required columns: %v", header)
	}

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
