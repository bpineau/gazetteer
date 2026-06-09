package filoiris

import (
	"embed"
	"io"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/filoiris.json.gz
var embedFS embed.FS

// set binds the embedded IRIS-Filosofi extract to the datadir/refresh
// pipeline. Refresh downloads the upstream INSEE zip and rebuilds the
// indexed (gzipped) JSON.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "filoiris.json.gz"},
	Raw:       []dataset.File{{Name: rawZipName, URL: rawZipURL}},
	Transform: transform,
	Validate:  validate,
}

// Entry carries the per-IRIS INSEE-Filosofi 2021 disposable-income
// indicators.
type Entry struct {
	MedianEUR      int     `json:"median_eur"`
	PovertyRatePct float64 `json:"poverty_rate_pct,omitempty"`
	Gini           float64 `json:"gini,omitempty"`
}

// Meta carries the manifest metadata for the IRIS-Filosofi dataset.
type Meta struct {
	Source            string `json:"source"`
	DownloadedAt      string `json:"downloaded_at"`
	DataYear          int    `json:"data_year"`
	RowCountIRIS      int    `json:"row_count_iris"`
	NationalMedianEUR int    `json:"national_median_eur"`
	Note              string `json:"note"`
}

// Index carries the per-IRIS Filosofi indicators with a national-median
// sanity scalar in the Meta block.
type Index struct {
	Meta Meta             `json:"meta"`
	IRIS map[string]Entry `json:"iris"`
}

var lazyIndex dataset.Lazy[Index]

// Load returns the singleton index, resolving the processed artifact from
// dir (the datadir) with a fallback to the embedded copy, parsed on first
// call. Subsequent calls are constant-time and ignore dir — the dir from
// the first call wins for the process lifetime. A dataset that is neither
// in the datadir nor embedded yields an empty index (graceful
// degradation), not an error.
func Load(dir string) (*Index, error) {
	return lazyIndex.Load(set, dir, parseIndex)
}

// parseIndex decodes the gzipped JSON extract into an Index.
func parseIndex(r io.Reader) (*Index, error) {
	return dataset.ReadGzJSON[Index](r, Name)
}

// Lookup returns the Filosofi entry for the given IRIS code. ok is false
// when the IRIS is absent (outside the ≥5000-inhabitant perimeter, or
// suppressed for statistical secrecy).
func (idx *Index) Lookup(iris string) (Entry, bool) {
	if idx == nil {
		return Entry{}, false
	}
	iris = strings.TrimSpace(iris)
	if iris == "" {
		return Entry{}, false
	}
	e, ok := idx.IRIS[iris]
	return e, ok
}

// Count returns the number of IRIS with at least the median indicator
// populated.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.IRIS)
}
