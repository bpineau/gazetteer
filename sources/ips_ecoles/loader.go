package ips_ecoles

import (
	"embed"
	"io"
	"strings"

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
