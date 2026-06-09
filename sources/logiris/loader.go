package logiris

import (
	"embed"
	"io"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/logiris.json.gz
var embedFS embed.FS

// set binds the embedded IRIS-logement extract to the datadir/refresh
// pipeline. Refresh downloads the upstream INSEE zip and rebuilds the
// indexed (gzipped) JSON.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "logiris.json.gz"},
	Raw:       []dataset.File{{Name: rawZipName, URL: rawZipURL}},
	Transform: transform,
	Validate:  validate,
}

// Entry carries the per-IRIS INSEE census housing-structure indicators.
type Entry struct {
	RenterSharePct        float64 `json:"renter_share_pct"`
	SocialHousingSharePct float64 `json:"social_housing_share_pct,omitempty"`
	VacancyRatePct        float64 `json:"vacancy_rate_pct"`
	TotalLogements        int     `json:"total_logements"`
}

// Meta carries the manifest metadata for the IRIS-logement dataset.
type Meta struct {
	Source       string `json:"source"`
	DownloadedAt string `json:"downloaded_at"`
	DataYear     int    `json:"data_year"`
	RowCountIRIS int    `json:"row_count_iris"`
	Scope        string `json:"scope"`
	Note         string `json:"note"`
}

// Index carries the per-IRIS housing indicators.
type Index struct {
	Meta Meta             `json:"meta"`
	IRIS map[string]Entry `json:"iris"`
}

var lazyIndex dataset.Lazy[Index]

// Load returns the singleton index, resolving the processed artifact from
// dir (the datadir) with a fallback to the embedded copy, parsed on first
// call. A dataset that is neither in the datadir nor embedded yields an
// empty index (graceful degradation), not an error.
func Load(dir string) (*Index, error) {
	return lazyIndex.Load(set, dir, parseIndex)
}

// parseIndex decodes the gzipped JSON extract into an Index.
func parseIndex(r io.Reader) (*Index, error) {
	return dataset.ReadGzJSON[Index](r, Name)
}

// Lookup returns the housing entry for the given IRIS code. ok is false
// when the IRIS is absent (outside the IDF scope, or no résidences
// principales).
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

// Count returns the number of IRIS in the index.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.IRIS)
}
