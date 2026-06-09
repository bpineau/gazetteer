package sitadel

import (
	"embed"
	"io"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/sitadel.json.gz
var embedFS embed.FS

// set binds the embedded SDES Sitadel extract to the datadir/refresh
// pipeline. Refresh downloads the upstream DIDO CSV and rebuilds the gzipped
// JSON via transform.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "sitadel.json.gz"},
	Raw:       []dataset.File{{Name: rawName, URL: rawURL}},
	Transform: transform,
	Validate:  validate,
}

// missing is the sentinel stored in a per-year array for a BLANK upstream
// cell (no data for that measure that year — distinct from a real 0).
const missing = -1

// Entry is one commune's compact per-year construction record. The three
// slices are parallel and aligned to YearStart: index i is the year
// YearStart+i. A cell of `missing` (-1) means the upstream value was blank
// (no data), which is kept distinct from a real 0.
type Entry struct {
	// YearStart is the year of index 0 in the slices below.
	YearStart int `json:"y0"`
	// Auth is "Tous Logements" LOG_AUT (dwellings authorised) per year.
	Auth []int `json:"a"`
	// Started is "Tous Logements" LOG_COM (dwellings started) per year;
	// `missing` for a blank cell (e.g. the provisional final millésime).
	Started []int `json:"s"`
	// CollAuth is "Collectif" LOG_AUT (apartment dwellings authorised) per
	// year; `missing` for a blank cell.
	CollAuth []int `json:"c"`
}

// Meta carries the manifest metadata for the embedded extract.
type Meta struct {
	Source           string `json:"source"`
	DataMillesime    string `json:"data_millesime"`
	RowCountCommunes int    `json:"row_count_communes"`
	Note             string `json:"note,omitempty"`
}

// Index is the per-INSEE lookup index.
type Index struct {
	Meta     Meta             `json:"meta"`
	Communes map[string]Entry `json:"communes"`
}

var lazyIndex dataset.Lazy[Index]

// Load returns the singleton index, resolving the processed artifact from dir
// (the datadir) with a fallback to the embedded copy, and parsing it on first
// call. Subsequent calls are constant-time and ignore dir — the dir from the
// first call wins for the process lifetime. A dataset that is neither in the
// datadir nor embedded yields an empty index (graceful degradation), not an
// error.
func Load(dir string) (*Index, error) {
	return lazyIndex.Load(set, dir, parseIndex)
}

// parseIndex decodes the gzipped JSON extract into an Index.
func parseIndex(r io.Reader) (*Index, error) {
	idx, err := dataset.ReadGzJSON[Index](r, Name)
	if err != nil {
		return nil, err
	}
	if idx.Communes == nil {
		idx.Communes = map[string]Entry{}
	}
	return idx, nil
}

// Lookup returns the entry for the given INSEE. `ok` is false when the commune
// is absent. The caller is responsible for folding Paris/Lyon/Marseille
// arrondissements onto their parent commune before calling.
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

// Count returns the number of communes in the loaded extract.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.Communes)
}
