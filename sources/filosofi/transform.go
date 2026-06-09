package filosofi

import (
	"context"
	"encoding/csv"
	"encoding/json"
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

// rawCSVName is the datadir filename for the upstream raw input.
const rawCSVName = "filosofi.raw.csv"

// rawCSVURL is the "Revenu des français à la commune" CSV on data.gouv.fr
// (slug revenu-des-francais-a-la-commune), an INSEE Filosofi aggregation.
// data.gouv mints a dated static path per revision; bump this when INSEE
// publishes a new Filosofi vintage (and bump dataYear accordingly).
const rawCSVURL = "https://static.data.gouv.fr/resources/revenu-des-francais-a-la-commune/20251210-134014/revenu-des-francais-a-la-commune-1765372688826.csv"

const metaSource = "data.gouv.fr/datasets/revenu-des-francais-a-la-commune/ (Filosofi 2021, INSEE)"

// dataYear is the Filosofi vintage of the upstream resource. The CSV does
// not carry it inline; keep it in sync with the resource above.
const dataYear = 2021

// Upstream column headers (Filosofi "disponible" — [DISP] — block).
const (
	colINSEE  = "Code géographique"
	colMedian = "[DISP] Médiane (€)"
	colMinima = "[DISP] dont part des minima sociaux (%)"
)

// transform rebuilds the processed filosofi artifact from the upstream CSV.
// It keeps communes with a published median disposable income (small
// communes are suppressed for statistical secrecy and carry an empty cell),
// records the optional "part des minima sociaux", and derives the national
// figure as the median of commune medians (matching the published artifact).
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawCSVName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	cr := csv.NewReader(dataset.BOMReader(rc))
	cr.Comma = ';'
	cr.FieldsPerRecord = -1

	header, err := cr.Read()
	if err != nil {
		return fmt.Errorf("filosofi: read header: %w", err)
	}
	insee, median, minima := indexOf(header, colINSEE), indexOf(header, colMedian), indexOf(header, colMinima)
	if insee < 0 || median < 0 {
		return fmt.Errorf("filosofi: header missing %q/%q: %v", colINSEE, colMedian, header)
	}

	idx := Index{
		Meta: Meta{
			Source:       metaSource,
			DataYear:     dataYear,
			Note:         "median_eur = revenu disponible médian annuel par UC (€). minima_pct = part des minima sociaux (%). national_median_eur = median of commune medians.",
			DownloadedAt: time.Now().UTC().Format("2006-01-02"),
		},
		Communes: map[string]Entry{},
	}
	var medians []int
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("filosofi: read row: %w", err)
		}
		code := strings.TrimSpace(rec[insee])
		if len(code) != 5 {
			continue // national/department aggregate rows, or blanks
		}
		med, ok := parseEuro(rec[median])
		if !ok {
			continue // suppressed (statistical secrecy)
		}
		e := Entry{MedianEUR: med}
		if minima >= 0 {
			if pct, ok := parsePct(rec[minima]); ok {
				e.MinimaPct = pct
			}
		}
		idx.Communes[code] = e
		medians = append(medians, med)
	}
	if len(idx.Communes) == 0 {
		return errors.New("filosofi: transform produced no communes")
	}
	idx.Meta.RowCountCommunes = len(idx.Communes)
	idx.Meta.NationalMedianEUR = stats.MedianInt(medians)

	return json.NewEncoder(dst).Encode(idx)
}

// validate gates publication: the rebuilt artifact must parse and be
// non-empty.
func validate(r io.Reader) error {
	idx, err := parseIndex(r)
	if err != nil {
		return err
	}
	if idx.Count() == 0 {
		return errors.New("filosofi: validated artifact has no communes")
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

// parseEuro parses a euro amount ("25820", "25 820", "25 820,0") into a
// rounded integer. ok is false for empty/suppressed cells.
func parseEuro(s string) (int, bool) {
	f, ok := frnorm.ParseFRFloat(s)
	if !ok {
		return 0, false
	}
	return int(math.Round(f)), true
}

// parsePct parses a French-formatted percentage; ok is false when empty.
func parsePct(s string) (float64, bool) { return frnorm.ParseFRFloat(s) }
