package cartofriches

import (
	"embed"
	"io"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/cartofriches_communes.json.gz
var embedFS embed.FS

// set binds the embedded Cartofriches extract to the datadir/refresh
// pipeline. Refresh downloads the Cerema friches-standard CSV (one row per
// referenced site) and rebuilds the per-commune aggregate via transform.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "cartofriches_communes.json.gz"},
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

// parseIndex decodes the gzipped-JSON extract into an Index.
func parseIndex(r io.Reader) (*Index, error) {
	return dataset.ReadGzJSON[Index](r, Name)
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
