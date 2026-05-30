package filoiris

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

var (
	indexOnce  sync.Once
	indexCache *Index
	indexErr   error
)

// Load returns the singleton index, resolving the processed artifact from
// dir (the datadir) with a fallback to the embedded copy, parsed on first
// call. Subsequent calls are constant-time and ignore dir. A dataset that
// is neither in the datadir nor embedded yields an empty index (graceful
// degradation), not an error.
func Load(dir string) (*Index, error) {
	indexOnce.Do(func() {
		rc, err := set.Open(dir)
		if errors.Is(err, dataset.ErrUnavailable) {
			indexCache = &Index{}
			return
		}
		if err != nil {
			indexErr = fmt.Errorf("filoiris: open dataset: %w", err)
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
		return nil, fmt.Errorf("filoiris: gunzip: %w", err)
	}
	defer func() { _ = zr.Close() }()
	body, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("filoiris: read gunzipped body: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("filoiris: parse filoiris json: %w", err)
	}
	return &idx, nil
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
