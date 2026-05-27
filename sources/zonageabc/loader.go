package zonageabc

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed data/zonage_abc_communes.json
var zonageABCFS embed.FS

// Meta carries the manifest metadata for the embedded dataset.
type Meta struct {
	Source           string `json:"source"`
	DownloadedAt     string `json:"downloaded_at"`
	EffectiveDate    string `json:"effective_date"`
	RowCountCommunes int    `json:"row_count_communes"`
	Note             string `json:"note"`
}

// Index carries the per-commune zonage classification.
type Index struct {
	Meta     Meta            `json:"meta"`
	Communes map[string]Zone `json:"communes"`
}

var (
	indexOnce  sync.Once
	indexCache *Index
	indexErr   error
)

// Load returns the singleton zonage ABC index. Parses the embedded
// JSON on first call; subsequent calls are constant-time.
func Load() (*Index, error) {
	indexOnce.Do(func() {
		raw, err := zonageABCFS.ReadFile("data/zonage_abc_communes.json")
		if err != nil {
			indexErr = fmt.Errorf("zonageabc: read dataset: %w", err)
			return
		}
		var idx Index
		if err := json.Unmarshal(raw, &idx); err != nil {
			indexErr = fmt.Errorf("zonageabc: parse dataset: %w", err)
			return
		}
		indexCache = &idx
	})
	return indexCache, indexErr
}

// Lookup returns the zonage classification for the given INSEE. `ok`
// is false when the commune is absent (rare; only fires for INSEE
// codes that do not exist in the September 2025 revision).
func (idx *Index) Lookup(insee string) (Zone, bool) {
	if idx == nil {
		return ZoneUnknown, false
	}
	insee = strings.TrimSpace(insee)
	if insee == "" {
		return ZoneUnknown, false
	}
	z, ok := idx.Communes[insee]
	if !ok {
		return ZoneUnknown, false
	}
	return z, true
}

// Count returns the number of communes in the dataset.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.Communes)
}
