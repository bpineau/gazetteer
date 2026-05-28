package ips_ecoles

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

//go:embed data/ips_ecoles_communes.json.gz
var ipsFS embed.FS

// Entry is one commune's row from the DEPP IPS dataset.
type Entry struct {
	IPSMedian   float64 `json:"ips_median"`
	IPSMin      float64 `json:"ips_min,omitempty"`
	IPSMax      float64 `json:"ips_max,omitempty"`
	SchoolCount int     `json:"school_count"`
}

// Meta carries the manifest metadata for the embedded extract.
type Meta struct {
	Source           string `json:"source"`
	DataYearLabel    string `json:"data_year_label"`
	RowCountCommunes int    `json:"row_count_communes"`
	RowCountSchools  int    `json:"row_count_schools"`
	Note             string `json:"note,omitempty"`
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

// Load returns the singleton index, parsing the embedded gzipped JSON
// on first call. Subsequent calls are constant-time.
func Load() (*Index, error) {
	indexOnce.Do(func() {
		raw, err := ipsFS.ReadFile("data/ips_ecoles_communes.json.gz")
		if err != nil {
			indexErr = fmt.Errorf("ips_ecoles: read embed: %w", err)
			return
		}
		zr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			indexErr = fmt.Errorf("ips_ecoles: gunzip: %w", err)
			return
		}
		defer func() { _ = zr.Close() }()
		body, err := io.ReadAll(zr)
		if err != nil {
			indexErr = fmt.Errorf("ips_ecoles: read gunzipped body: %w", err)
			return
		}
		var idx Index
		if err := json.Unmarshal(body, &idx); err != nil {
			indexErr = fmt.Errorf("ips_ecoles: parse json: %w", err)
			return
		}
		indexCache = &idx
	})
	return indexCache, indexErr
}

// Lookup returns the entry for the given INSEE. `ok` is false when the
// commune hosts no école in the dataset (rural communes with zero
// école primaire are the bulk of misses).
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

// Count returns the number of communes hosting ≥ 1 école in the
// embedded crosswalk.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.Communes)
}
