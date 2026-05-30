package bpe

import (
	"compress/gzip"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/bpe_communes.json.gz
var embedFS embed.FS

// set binds the embedded extract to the datadir/refresh pipeline. The
// Transform is not yet reconstructed, so the Set is read-only: Open
// resolves datadir > embed, and refresh reports it as skipped.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "bpe_communes.json.gz"},
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
			indexErr = fmt.Errorf("bpe: open dataset: %w", err)
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

// parseIndex decodes the gzipped JSON extract into an Index.
func parseIndex(r io.Reader) (*Index, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("bpe: gunzip header: %w", err)
	}
	defer func() { _ = gz.Close() }()

	payload, err := io.ReadAll(gz)
	if err != nil {
		return nil, fmt.Errorf("bpe: gunzip body: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(payload, &idx); err != nil {
		return nil, fmt.Errorf("bpe: parse json: %w", err)
	}
	return &idx, nil
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
