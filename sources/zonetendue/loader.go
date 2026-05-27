package zonetendue

import (
	"embed"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

//go:embed data/zonage_tlv_communes.json
var zonetendueFS embed.FS

// Entry stores the per-commune TLV + zone-tendue flags.
type Entry struct {
	TLV2013 bool `json:"tlv_2013,omitempty"`
	Tier    Tier `json:"tlv"`
}

// Meta carries the manifest metadata for the embedded dataset.
type Meta struct {
	Source           string `json:"source"`
	DownloadedAt     string `json:"downloaded_at"`
	EffectiveDate    string `json:"effective_date"`
	RowCountCommunes int    `json:"row_count_communes"`
	RowCountKept     int    `json:"row_count_kept"`
	Note             string `json:"note"`
}

// Index carries the per-commune classification. Only communes with a
// non-default tier are stored ; the rest are implicitly TierNonTendue.
type Index struct {
	Meta     Meta             `json:"meta"`
	Communes map[string]Entry `json:"communes"`
}

var (
	indexOnce  sync.Once
	indexCache *Index
	indexErr   error
)

// Load returns the singleton zone-tendue index. Parses the embedded
// JSON on first call; subsequent calls are constant-time.
func Load() (*Index, error) {
	indexOnce.Do(func() {
		raw, err := zonetendueFS.ReadFile("data/zonage_tlv_communes.json")
		if err != nil {
			indexErr = fmt.Errorf("zonetendue: read dataset: %w", err)
			return
		}
		var idx Index
		if err := json.Unmarshal(raw, &idx); err != nil {
			indexErr = fmt.Errorf("zonetendue: parse dataset: %w", err)
			return
		}
		indexCache = &idx
	})
	return indexCache, indexErr
}

// Lookup returns the entry for the given INSEE. `ok` is false when
// the commune is absent ; absence semantically means TierNonTendue.
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

// CountTendue returns the number of communes explicitly stored in the
// dataset.
func (idx *Index) CountTendue() int {
	if idx == nil {
		return 0
	}
	return len(idx.Communes)
}
