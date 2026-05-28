package rpls

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

//go:embed data/rpls_communes.json.gz
var rplsFS embed.FS

// Entry is one commune's row from the SRU dataset.
type Entry struct {
	Label   string  `json:"label,omitempty"`
	RatePct float64 `json:"rate_pct"`
}

// Meta carries the manifest metadata for the embedded extract.
type Meta struct {
	Source           string `json:"source"`
	DataYear         int    `json:"data_year"`
	RowCountCommunes int    `json:"row_count_communes"`
	Note             string `json:"note,omitempty"`
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
		raw, err := rplsFS.ReadFile("data/rpls_communes.json.gz")
		if err != nil {
			indexErr = fmt.Errorf("rpls: read embed: %w", err)
			return
		}
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			indexErr = fmt.Errorf("rpls: gunzip: %w", err)
			return
		}
		defer func() { _ = zr.Close() }()
		body, err := io.ReadAll(zr)
		if err != nil {
			indexErr = fmt.Errorf("rpls: read gunzipped body: %w", err)
			return
		}
		var idx Index
		if err := json.Unmarshal(body, &idx); err != nil {
			indexErr = fmt.Errorf("rpls: parse json: %w", err)
			return
		}
		indexCache = &idx
	})
	return indexCache, indexErr
}

// Lookup returns the entry for the given INSEE. `ok` is false when the
// commune is absent (rare — DOM-COM communes that are not in the SRU
// inventory).
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

// Count returns the number of communes in the embedded crosswalk.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.Communes)
}
