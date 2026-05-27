package delinquance

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

//go:embed data/delinquance_communes.json.gz
var delinquanceFS embed.FS

// Entry is one commune's row from the SSMSI dataset.
type Entry struct {
	// Population is the INSEE-published resident population the SSMSI
	// uses as the rate denominator.
	Population int `json:"pop"`
	// Rates maps indicator handles to per-thousand rates (events per
	// 1 000 inhabitants, or per 1 000 logements for burglary).
	Rates map[string]float64 `json:"ind"`
}

// Meta carries the manifest metadata for the embedded extract.
type Meta struct {
	Source           string   `json:"source"`
	DataYear         int      `json:"data_year"`
	Unit             string   `json:"unit"`
	RowCountCommunes int      `json:"row_count_communes"`
	Indicators       []string `json:"indicators"`
	Note             string   `json:"note"`
}

// Index is the per-INSEE lookup index.
type Index struct {
	Meta     Meta             `json:"meta"`
	Communes map[string]Entry `json:"communes"`
}

var (
	indexOnce  sync.Once
	indexCache *Index
	indexErr   error
)

// Load returns the singleton index, parsing the embedded gzipped JSON
// on first call. Subsequent calls are constant-time.
func Load() (*Index, error) {
	indexOnce.Do(func() {
		raw, err := delinquanceFS.ReadFile("data/delinquance_communes.json.gz")
		if err != nil {
			indexErr = fmt.Errorf("delinquance: read embed: %w", err)
			return
		}
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			indexErr = fmt.Errorf("delinquance: gunzip: %w", err)
			return
		}
		defer func() { _ = zr.Close() }()
		body, err := io.ReadAll(zr)
		if err != nil {
			indexErr = fmt.Errorf("delinquance: read gunzipped body: %w", err)
			return
		}
		var idx Index
		if err := json.Unmarshal(body, &idx); err != nil {
			indexErr = fmt.Errorf("delinquance: parse json: %w", err)
			return
		}
		indexCache = &idx
	})
	return indexCache, indexErr
}

// Lookup returns the entry for the given INSEE. `ok` is false when
// the commune is absent (rare — typically the smallest communes with
// every indicator masked by the secret-statistique rule).
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

// Count returns the number of communes with at least one indicator
// populated.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.Communes)
}
