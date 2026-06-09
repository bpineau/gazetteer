package filosofi

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/filosofi_communes.json
var embedFS embed.FS

// set binds the embedded Filosofi extract to the datadir/refresh pipeline.
// Refresh downloads the upstream CSV and rebuilds the indexed JSON.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "filosofi_communes.json"},
	Raw:       []dataset.File{{Name: rawCSVName, URL: rawCSVURL}},
	Transform: transform,
	Validate:  validate,
}

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

// parseIndex decodes the JSON extract into an Index.
func parseIndex(r io.Reader) (*Index, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("filosofi: read filosofi_communes: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("filosofi: parse filosofi_communes: %w", err)
	}
	return &idx, nil
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
