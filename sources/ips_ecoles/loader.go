package ips_ecoles

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

//go:embed data/ips_ecoles_communes.json.gz
var embedFS embed.FS

// set binds the embedded extract to the datadir/refresh pipeline. Open
// resolves datadir > embed; Refresh downloads the DEPP CSV (rawCSVName) and
// rebuilds the gzipped per-commune artifact via transform, gated by validate.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "ips_ecoles_communes.json.gz"},
	Raw: []dataset.File{
		{Name: rawCSVName, URL: rawCSVURL},
	},
	Transform: transform,
	Validate:  validate,
}

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
			indexErr = fmt.Errorf("ips_ecoles: open dataset: %w", err)
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
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("ips_ecoles: gunzip: %w", err)
	}
	defer func() { _ = zr.Close() }()
	body, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("ips_ecoles: read gunzipped body: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("ips_ecoles: parse json: %w", err)
	}
	return &idx, nil
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
