package carteloyers

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"

	"github.com/bpineau/gazetteer/dataset"
)

// Upstream "Carte des loyers" CSVs (DHUP/ANIL, INRAE model), 2025 vintage,
// published on data.gouv.fr (slug carte-des-loyers-indicateurs-de-loyers-
// dannonce-par-commune-en-2025). One file per typology; data.gouv mints a
// dated static path per vintage — bump these (and the embedded data) when a
// new vintage ships.
const (
	urlAppartement = "https://static.data.gouv.fr/resources/carte-des-loyers-indicateurs-de-loyers-dannonce-par-commune-en-2025/20251211-145010/pred-app-mef-dhup.csv"
	urlMaison      = "https://static.data.gouv.fr/resources/carte-des-loyers-indicateurs-de-loyers-dannonce-par-commune-en-2025/20251211-145039/pred-mai-mef-dhup.csv"
	urlApt12       = "https://static.data.gouv.fr/resources/carte-des-loyers-indicateurs-de-loyers-dannonce-par-commune-en-2025/20251211-144934/pred-app12-mef-dhup.csv"
	urlApt3        = "https://static.data.gouv.fr/resources/carte-des-loyers-indicateurs-de-loyers-dannonce-par-commune-en-2025/20251211-144951/pred-app3-mef-dhup.csv"
)

// outHeader is the compact 7-column schema the source parses (see
// loader.go parseCSV). The upstream ships extra columns and names the two
// interval bounds with dots (lwr.IPm2 / upr.IPm2); the transform projects
// onto this header.
var outHeader = []string{"INSEE_C", "DEP", "loypredm2", "lwr_IPm2", "upr_IPm2", "TYPPRED", "nbobs_com"}

// upstreamCols maps each output column to its upstream header name.
var upstreamCols = map[string]string{
	"INSEE_C":   "INSEE_C",
	"DEP":       "DEP",
	"loypredm2": "loypredm2",
	"lwr_IPm2":  "lwr.IPm2",
	"upr_IPm2":  "upr.IPm2",
	"TYPPRED":   "TYPPRED",
	"nbobs_com": "nbobs_com",
}

// makeTransform returns a Transform that reads the named raw upstream CSV
// and re-emits the compact 7-column CSV the source loads. All four
// typologies share this logic; they differ only by their raw file.
//
// The upstream is Latin-1 and semicolon-delimited with quoted headers, and
// uses a comma decimal mark. We keep only ASCII columns (codes + numbers),
// so no charset conversion is needed; the comma decimals are preserved
// verbatim (a comma is not the field delimiter).
func makeTransform(rawName string) dataset.Transform {
	return func(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
		rc, err := raw.Open(rawName)
		if err != nil {
			return err
		}
		defer func() { _ = rc.Close() }()

		cr := csv.NewReader(rc)
		cr.Comma = ';'
		cr.FieldsPerRecord = -1

		header, err := cr.Read()
		if err != nil {
			return fmt.Errorf("carteloyers: read header: %w", err)
		}
		src := make([]int, len(outHeader))
		for i, out := range outHeader {
			src[i] = indexOf(header, upstreamCols[out])
			if src[i] < 0 {
				return fmt.Errorf("carteloyers: upstream missing column %q for %q", upstreamCols[out], out)
			}
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
				return fmt.Errorf("carteloyers: read row: %w", err)
			}
			row := make([]string, len(outHeader))
			for i, idx := range src {
				if idx < len(rec) {
					row[i] = rec[idx]
				}
			}
			if row[0] == "" {
				continue // drop rows without an INSEE code
			}
			if err := w.Write(row); err != nil {
				return err
			}
			n++
		}
		w.Flush()
		if err := w.Error(); err != nil {
			return err
		}
		if n == 0 {
			return errors.New("carteloyers: transform produced no rows")
		}
		return nil
	}
}

// validate gates publication: the rebuilt CSV must parse and be non-empty.
func validate(r io.Reader) error {
	rows, err := parseCSV(r)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return errors.New("carteloyers: validated artifact has no rows")
	}
	return nil
}

// indexOf returns the index of the column whose trimmed header equals name,
// or -1. The upstream quotes its headers; csv strips the quotes.
func indexOf(header []string, name string) int {
	for i, h := range header {
		if h == name {
			return i
		}
	}
	return -1
}
