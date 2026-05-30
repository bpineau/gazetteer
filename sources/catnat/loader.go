package catnat

import (
	"compress/gzip"
	"embed"
	"encoding/json"
	"errors"
	"sync"

	"github.com/bpineau/gazetteer/dataset"
)

//go:embed data/catnat.json.gz
var embedFS embed.FS

// set binds the embedded aggregate to its upstream GASPAR export.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "catnat.json.gz"},
	Raw:       []dataset.File{{Name: rawName, URL: rawURL}},
	Transform: transform,
	Validate:  validate,
}

// processed is the embedded artifact: per-commune CatNat aggregates plus the
// reference vintage the recent window is measured against.
type processed struct {
	RefYear     int     `json:"ref_year"`
	WindowYears int     `json:"window_years"`
	Communes    []Entry `json:"communes"`
}

// Entry is one commune's aggregated CatNat history.
type Entry struct {
	INSEE    string `json:"insee"`
	Total    int    `json:"total"`
	Recent   int    `json:"recent"`
	LastYear int    `json:"last_year"`
	Inond    int    `json:"inond,omitempty"`
	Sech     int    `json:"sech,omitempty"`
	Mvt      int    `json:"mvt,omitempty"`
	Temp     int    `json:"temp,omitempty"`
}

// Index is the in-memory lookup built from the processed artifact.
type Index struct {
	byINSEE     map[string]Entry
	refYear     int
	windowYears int
}

// Lookup returns the aggregate for a commune, or (zero, false) when absent.
func (idx *Index) Lookup(insee string) (Entry, bool) {
	if idx == nil {
		return Entry{}, false
	}
	r, ok := idx.byINSEE[insee]
	return r, ok
}

// Count reports how many communes the index covers.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.byINSEE)
}

var (
	indexOnce  sync.Once
	indexCache *Index
	indexErr   error
)

// Load returns the singleton lookup index, resolving the artifact from dir (the
// datadir) with a fallback to the embedded snapshot, parsed on first call. A
// missing (non-embedded) artifact yields an empty index rather than an error.
func Load(dir string) (*Index, error) {
	indexOnce.Do(func() {
		indexCache, indexErr = parse(dir)
	})
	return indexCache, indexErr
}

func parse(dir string) (*Index, error) {
	rc, err := set.Open(dir)
	if errors.Is(err, dataset.ErrUnavailable) {
		return &Index{byINSEE: map[string]Entry{}}, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	gz, err := gzip.NewReader(rc)
	if err != nil {
		return nil, err
	}
	defer func() { _ = gz.Close() }()

	var p processed
	if err := json.NewDecoder(gz).Decode(&p); err != nil {
		return nil, err
	}
	idx := &Index{
		byINSEE:     make(map[string]Entry, len(p.Communes)),
		refYear:     p.RefYear,
		windowYears: p.WindowYears,
	}
	for _, c := range p.Communes {
		idx.byINSEE[c.INSEE] = c
	}
	return idx, nil
}
