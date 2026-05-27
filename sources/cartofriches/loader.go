package cartofriches

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed data/cartofriches_communes.json
var cartofrichesFS embed.FS

// Entry is one commune's aggregate row.
type Entry struct {
	Label          string         `json:"label,omitempty"`
	SiteCount      int            `json:"n"`
	ByType         map[string]int `json:"by_type,omitempty"`
	ByStatus       map[string]int `json:"by_status,omitempty"`
	TotalSurfaceM2 int            `json:"total_surface_m2,omitempty"`
}

// Meta carries the manifest metadata for the embedded extract.
type Meta struct {
	Source           string `json:"source"`
	RowCountCommunes int    `json:"row_count_communes"`
	RowCountSites    int    `json:"row_count_sites"`
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
		raw, err := cartofrichesFS.ReadFile("data/cartofriches_communes.json")
		if err != nil {
			indexErr = fmt.Errorf("cartofriches: read embed: %w", err)
			return
		}
		var idx Index
		if err := json.Unmarshal(raw, &idx); err != nil {
			indexErr = fmt.Errorf("cartofriches: parse json: %w", err)
			return
		}
		indexCache = &idx
	})
	return indexCache, indexErr
}

// Lookup returns the aggregate entry for the given INSEE. `ok` is
// false when the commune hosts no Cartofriches-referenced site.
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

// Count returns the number of communes hosting at least one site.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.Communes)
}
