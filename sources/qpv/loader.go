package qpv

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed data/qpv_communes.json
var qpvFS embed.FS

// Entry is one commune's row.
type Entry struct {
	Label string `json:"label,omitempty"`
	QPVs  []QPV  `json:"qpvs,omitempty"`
}

// Meta carries the manifest metadata for the embedded extract.
type Meta struct {
	Source           string `json:"source"`
	RowCountCommunes int    `json:"row_count_communes"`
	RowCountQPV      int    `json:"row_count_qpv"`
	Note             string `json:"note"`
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

// Load returns the singleton index, parsing the embedded JSON on
// first call. Subsequent calls are constant-time.
func Load() (*Index, error) {
	indexOnce.Do(func() {
		raw, err := qpvFS.ReadFile("data/qpv_communes.json")
		if err != nil {
			indexErr = fmt.Errorf("qpv: read embed: %w", err)
			return
		}
		var idx Index
		if err := json.Unmarshal(raw, &idx); err != nil {
			indexErr = fmt.Errorf("qpv: parse json: %w", err)
			return
		}
		indexCache = &idx
	})
	return indexCache, indexErr
}

// Lookup returns the entry for the given INSEE. `ok` is false when
// the commune hosts no QPV.
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

// Count returns the number of communes hosting at least one QPV.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.Communes)
}
