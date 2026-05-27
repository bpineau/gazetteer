package bpe

import (
	"bytes"
	"compress/gzip"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

//go:embed data/bpe_communes.json.gz
var bpeFS embed.FS

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

// Load returns the singleton index, gunzipping + parsing the embedded
// JSON on first call. Subsequent calls are constant-time.
func Load() (*Index, error) {
	indexOnce.Do(func() {
		raw, err := bpeFS.ReadFile("data/bpe_communes.json.gz")
		if err != nil {
			indexErr = fmt.Errorf("bpe: read embed: %w", err)
			return
		}
		gz, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			indexErr = fmt.Errorf("bpe: gunzip header: %w", err)
			return
		}
		defer func() { _ = gz.Close() }()

		payload, err := io.ReadAll(gz)
		if err != nil {
			indexErr = fmt.Errorf("bpe: gunzip body: %w", err)
			return
		}
		var idx Index
		if err := json.Unmarshal(payload, &idx); err != nil {
			indexErr = fmt.Errorf("bpe: parse json: %w", err)
			return
		}
		indexCache = &idx
	})
	return indexCache, indexErr
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
