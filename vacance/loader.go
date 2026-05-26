package vacance

import (
	"bytes"
	"embed"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

//go:embed data/vacance_communes.csv
var vacanceFS embed.FS

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

// Load returns the singleton index, parsing the embedded CSV on
// first call.
func Load() (*Index, error) {
	indexOnce.Do(func() {
		raw, err := vacanceFS.ReadFile("data/vacance_communes.csv")
		if err != nil {
			indexErr = fmt.Errorf("vacance: read vacance_communes: %w", err)
			return
		}
		idx, err := parseCSV(raw)
		if err != nil {
			indexErr = fmt.Errorf("vacance: parse vacance_communes: %w", err)
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

func parseCSV(raw []byte) (*Index, error) {
	r := csv.NewReader(bytes.NewReader(raw))
	r.Comma = ';'
	r.FieldsPerRecord = -1
	header, err := r.Read()
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
		rec, err := r.Read()
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
