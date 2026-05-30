package lovac

import (
	"embed"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"

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
	VacancePct     float64 // taux de logements vacants 2025 (parc privé)
	VacanceLongPct float64 // taux de logements vacants > 2 ans 2025
}

// Index is the per-INSEE lookup index.
type Index struct {
	byInsee map[string]Entry
}

var (
	indexOnce  sync.Once
	indexCache *Index
	indexErr   error
)

// Load returns the singleton index, resolving the processed artifact from
// dir (the datadir) with a fallback to the embedded copy, and parsing it on
// first call. Subsequent calls are constant-time and ignore dir — the dir
// from the first call wins for the process lifetime. A dataset that is
// neither in the datadir nor embedded yields an empty index (graceful
// degradation), not an error.
func Load(dir string) (*Index, error) {
	indexOnce.Do(func() {
		rc, err := set.Open(dir)
		if errors.Is(err, dataset.ErrUnavailable) {
			indexCache = &Index{}
			return
		}
		if err != nil {
			indexErr = fmt.Errorf("vacance: open dataset: %w", err)
			return
		}
		defer func() { _ = rc.Close() }()
		idx, err := parseIndex(rc)
		if err != nil {
			indexErr = err
			return
		}
		indexCache = idx
	})
	return indexCache, indexErr
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
	required := []string{"INSEE_C", "taux_vacance_25_pct", "taux_vacance_long_25_pct"}
	for _, name := range required {
		if _, ok := col[name]; !ok {
			return nil, fmt.Errorf("missing column %q in header %v", name, header)
		}
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
		insee := strings.TrimSpace(rec[col["INSEE_C"]])
		if insee == "" {
			continue
		}
		rateStr := strings.TrimSpace(rec[col["taux_vacance_25_pct"]])
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
		if s := strings.TrimSpace(rec[col["taux_vacance_long_25_pct"]]); s != "" {
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
