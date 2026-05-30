package zonetendue

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/zonage_tlv_communes.json
var embedFS embed.FS

// set binds the embedded extract to the datadir/refresh pipeline. The
// Transform is not yet reconstructed, so the Set is read-only: Open
// resolves datadir > embed, and refresh reports it as skipped.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "zonage_tlv_communes.json"},
}

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

// Load returns the singleton index, resolving the processed artifact from
// dir (the datadir) with a fallback to the embedded copy, and parsing it on
// first call. Subsequent calls are constant-time and ignore dir — the dir
// from the first call wins for the process lifetime. A dataset that is
// neither in the datadir nor embedded yields an empty index (graceful
// degradation), not an error.
func Load(dir string) (*Index, error) {
	indexOnce.Do(func() {
		rc, err := set.Open(dir)
		if errors.Is(err, dataset.ErrUnavailable) {
			indexCache = &Index{}
			return
		}
		if err != nil {
			indexErr = fmt.Errorf("zonetendue: open dataset: %w", err)
			return
		}
		defer func() { _ = rc.Close() }()
		idx, err := parseIndex(rc)
		if err != nil {
			indexErr = err
			return
		}
		indexCache = idx
	})
	return indexCache, indexErr
}

// parseIndex decodes the JSON extract into an Index.
func parseIndex(r io.Reader) (*Index, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("zonetendue: read dataset: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("zonetendue: parse dataset: %w", err)
	}
	return &idx, nil
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
