package bpe

import (
	"embed"
	"io"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/bpe_communes.json.gz
var embedFS embed.FS

// set binds the embedded extract to the datadir/refresh pipeline. Refresh
// downloads the INSEE BPE ZIP, rebuilds the curated per-commune bucket
// vectors via transform, and validates them before publishing; Open
// resolves datadir > embed.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "bpe_communes.json.gz"},
	Raw:       []dataset.File{{Name: rawName, URL: rawURL}},
	Transform: transform,
	Validate:  validate,
}

// Meta carries the manifest metadata for the embedded extract.
type Meta struct {
	Source           string         `json:"source"`
	ReferenceDate    string         `json:"reference_date"`
	RowCountCommunes int            `json:"row_count_communes"`
	BucketTotals     map[string]int `json:"bucket_totals,omitempty"`
	Note             string         `json:"note,omitempty"`
}

// Index is the per-INSEE lookup index.
type Index struct {
	Meta     Meta                      `json:"meta"`
	Communes map[string]map[Bucket]int `json:"communes"`
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

// parseIndex decodes the gzipped JSON extract into an Index.
func parseIndex(r io.Reader) (*Index, error) {
	return dataset.ReadGzJSON[Index](r, Name)
}

// Lookup returns the per-bucket counts map for the given INSEE.
// `ok` is false when the commune is absent from the embedded subset
// (the commune has zero facilities in the curated buckets).
func (idx *Index) Lookup(insee string) (map[Bucket]int, bool) {
	if idx == nil {
		return nil, false
	}
	insee = strings.TrimSpace(insee)
	if insee == "" {
		return nil, false
	}
	c, ok := idx.Communes[insee]
	return c, ok
}

// Count returns the number of communes in the embedded subset.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.Communes)
}
