package cartofriches

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

//go:embed data/cartofriches_communes.json
var embedFS embed.FS

// set binds the embedded Cartofriches extract to the datadir/refresh
// pipeline. Refresh downloads the Cerema friches-standard CSV (one row per
// referenced site) and rebuilds the per-commune aggregate via transform.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "cartofriches_communes.json"},
	Raw:       []dataset.File{{Name: rawName, URL: rawURL}},
	Transform: transform,
	Validate:  validate,
}

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
			indexErr = fmt.Errorf("cartofriches: open dataset: %w", err)
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
		return nil, fmt.Errorf("cartofriches: read embed: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("cartofriches: parse json: %w", err)
	}
	return &idx, nil
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
