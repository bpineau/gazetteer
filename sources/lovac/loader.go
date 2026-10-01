package lovac

import (
	"embed"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/lovac_communes.csv
var embedFS embed.FS

// set binds the embedded extract to the datadir/refresh pipeline. Refresh
// downloads the upstream LOVAC communal CSV and rebuilds the compact CSV.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "lovac_communes.csv"},
	Raw:       []dataset.File{{Name: rawCSVName, URL: rawCSVURL}},
	Transform: transform,
	Validate:  validate,
}

// Entry captures the LOVAC-derived taux de logements vacants for one
// commune. All percentages are 0..100 floats.
type Entry struct {
	InseeCode      string
	VacancePct     float64 // taux de logements vacants (parc privé), embedded LOVAC edition
	VacanceLongPct float64 // taux de logements vacants > 2 ans, same edition
}

// Index is the per-INSEE lookup index.
type Index struct {
	byInsee map[string]Entry
}

var lazyIndex dataset.Lazy[Index]

// Load returns the singleton index, resolving the processed artifact from
// dir (the datadir) with a fallback to the embedded copy, and parsing it on
// first call. Subsequent calls are constant-time and ignore dir — the dir
// from the first call wins for the process lifetime. A dataset that is
// neither in the datadir nor embedded yields an empty index (graceful
// degradation), not an error.
func Load(dir string) (*Index, error) {
	return lazyIndex.Load(set, dir, parseIndex)
}

// Lookup returns the vacance entry for the given INSEE. The `ok` flag
// is false when the commune was filtered out at LOVAC ingestion (small
// commune with masked statistics — "secret statistique" rule).
func (idx *Index) Lookup(insee string) (Entry, bool) {
	if idx == nil {
		return Entry{}, false
	}
	insee = strings.TrimSpace(insee)
	if insee == "" {
		return Entry{}, false
	}
	e, ok := idx.byInsee[insee]
	return e, ok
}

// Count returns the number of communes with usable observations.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.byInsee)
}

// parseIndex parses the semicolon-delimited CSV extract into an Index.
func parseIndex(r io.Reader) (*Index, error) {
	cr := csv.NewReader(r)
	cr.Comma = ';'
	cr.FieldsPerRecord = -1
	header, err := cr.Read()
	if err != nil {
		return nil, fmt.Errorf("read header: %w", err)
	}
	col := map[string]int{}
	for i, name := range header {
		col[strings.TrimSpace(name)] = i
	}
	// The rate columns carry no edition year since the 2026 edition; an
	// artifact built from the 2025 edition named them with a "_25_" infix.
	iInsee, okI := col[colOutINSEE]
	iRate, okR := firstColumn(col, colOutRate, legacyRateCol)
	iLong, okL := firstColumn(col, colOutLong, legacyLongCol)
	if !okI || !okR || !okL {
		return nil, fmt.Errorf("missing INSEE / rate columns in header %v", header)
	}
	out := make(map[string]Entry, 16_000)
	for {
		rec, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read row: %w", err)
		}
		insee := strings.TrimSpace(field(rec, iInsee))
		if insee == "" {
			continue
		}
		rateStr := strings.TrimSpace(field(rec, iRate))
		if rateStr == "" {
			// Pre-processed file may keep the row for the long-term
			// number even if the headline rate is masked. We skip
			// those — the headline rate is what the score uses.
			continue
		}
		rate, err := strconv.ParseFloat(rateStr, 64)
		if err != nil {
			continue
		}
		longRate := 0.0
		if s := strings.TrimSpace(field(rec, iLong)); s != "" {
			if v, err := strconv.ParseFloat(s, 64); err == nil {
				longRate = v
			}
		}
		out[insee] = Entry{
			InseeCode:      insee,
			VacancePct:     rate,
			VacanceLongPct: longRate,
		}
	}
	return &Index{byInsee: out}, nil
}

// firstColumn returns the index of the first of names present in col.
func firstColumn(col map[string]int, names ...string) (int, bool) {
	for _, n := range names {
		if i, ok := col[n]; ok {
			return i, true
		}
	}
	return 0, false
}

// field returns rec[i], or "" when the row is shorter than the header.
func field(rec []string, i int) string {
	if i < 0 || i >= len(rec) {
		return ""
	}
	return rec[i]
}
