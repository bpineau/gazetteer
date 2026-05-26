package filosofi

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed data/filosofi_communes.json
var filosofiFS embed.FS

// Entry carries the per-commune INSEE-Filosofi 2021 indicators.
// MedianEUR is the annual revenu disponible médian par UC (€).
// MinimaPct is the part des minima sociaux dans le revenu disponible
// (%) — used as a proxy for the (unavailable in the open commune CSV)
// taux de pauvreté TP60.
type Entry struct {
	MedianEUR int     `json:"median_eur"`
	MinimaPct float64 `json:"minima_pct,omitempty"`
}

// Meta carries the manifest metadata for the Filosofi dataset.
type Meta struct {
	Source            string `json:"source"`
	DownloadedAt      string `json:"downloaded_at"`
	DataYear          int    `json:"data_year"`
	RowCountCommunes  int    `json:"row_count_communes"`
	NationalMedianEUR int    `json:"national_median_eur"`
	Note              string `json:"note"`
}

// Index carries the per-commune Filosofi indicators with a national-median
// sanity scalar in the Meta block.
type Index struct {
	Meta     Meta             `json:"meta"`
	Communes map[string]Entry `json:"communes"`
}

var (
	indexOnce  sync.Once
	indexCache *Index
	indexErr   error
)

// Load returns the singleton Filosofi index. Parses the embedded JSON
// on first call; subsequent calls are constant-time.
func Load() (*Index, error) {
	indexOnce.Do(func() {
		raw, err := filosofiFS.ReadFile("data/filosofi_communes.json")
		if err != nil {
			indexErr = fmt.Errorf("filosofi: read filosofi_communes: %w", err)
			return
		}
		var idx Index
		if err := json.Unmarshal(raw, &idx); err != nil {
			indexErr = fmt.Errorf("filosofi: parse filosofi_communes: %w", err)
			return
		}
		indexCache = &idx
	})
	return indexCache, indexErr
}

// Lookup returns the Filosofi entry for the given INSEE. `ok` is false
// when the commune is absent (secret statistique : ~3500 communes
// under the 50-household threshold are dropped by INSEE).
func (idx *Index) Lookup(insee string) (Entry, bool) {
	if idx == nil {
		return Entry{}, false
	}
	insee = strings.TrimSpace(insee)
	if insee == "" {
		return Entry{}, false
	}
	e, ok := idx.Communes[insee]
	return e, ok
}

// Count returns the number of communes with at least the median
// indicator populated.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.Communes)
}
