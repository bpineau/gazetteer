package anct

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed data/anct_programmes.json
var anctFS embed.FS

// Entry is one commune's row from the merged ACV / PVD / ORT extract.
type Entry struct {
	Label string `json:"label,omitempty"`

	ACV         bool   `json:"acv,omitempty"`
	ACVSignedAt string `json:"acv_signed_at,omitempty"`

	PVD         bool   `json:"pvd,omitempty"`
	PVDSignedAt string `json:"pvd_signed_at,omitempty"`

	ORT         bool   `json:"ort,omitempty"`
	ORTSignedAt string `json:"ort_signed_at,omitempty"`
}

// Meta carries the manifest metadata for the embedded extract.
type Meta struct {
	Source           string `json:"source"`
	RowCountCommunes int    `json:"row_count_communes"`
	RowCountACV      int    `json:"row_count_acv"`
	RowCountPVD      int    `json:"row_count_pvd"`
	RowCountORT      int    `json:"row_count_ort"`
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
		raw, err := anctFS.ReadFile("data/anct_programmes.json")
		if err != nil {
			indexErr = fmt.Errorf("anct: read embed: %w", err)
			return
		}
		var idx Index
		if err := json.Unmarshal(raw, &idx); err != nil {
			indexErr = fmt.Errorf("anct: parse json: %w", err)
			return
		}
		indexCache = &idx
	})
	return indexCache, indexErr
}

// Lookup returns the entry for the given INSEE. `ok` is false when
// the commune participates in none of the three programmes.
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

// Count returns the number of communes flagged for at least one
// programme.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.Communes)
}
