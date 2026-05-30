package cdsr

import (
	"embed"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"sync"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/geodist"
)

//go:embed data/cdsr.json
var embedFS embed.FS

// set binds the embedded snapshot to its Opendatasoft upstream so the datadir
// override and refresh tooling operate on it.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "cdsr.json"},
	Raw:       []dataset.File{{Name: rawName, URL: rawURL}},
	Transform: transform,
	Validate:  validate,
}

// Copro is one CDSR-labelled condominium in the catalog.
type Copro struct {
	Name      string  `json:"name"`
	Address   string  `json:"address"`
	Commune   string  `json:"commune"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
	Lots      int     `json:"lots"`
	LabelYear int     `json:"label_year"`
}

// Catalog is the in-memory CDSR snapshot. The dataset is tiny (~17 copros), so
// a linear nearest-scan is more than fast enough — no spatial index.
type Catalog struct {
	Copros []Copro
}

// withinSorted returns the copros within maxM metres of (lat, lon), each paired
// with its haversine distance, sorted by ascending distance.
func (c *Catalog) withinSorted(lat, lon, maxM float64) []nearby {
	var out []nearby
	for i := range c.Copros {
		d := geodist.MetersBetween(lat, lon, c.Copros[i].Lat, c.Copros[i].Lon)
		if d <= maxM {
			out = append(out, nearby{copro: &c.Copros[i], meters: d})
		}
	}
	// Sort by ascending distance, breaking ties on name so the ordering (and
	// thus which items survive the maxNearestItems cap) is fully deterministic.
	sort.Slice(out, func(i, j int) bool {
		if out[i].meters != out[j].meters {
			return out[i].meters < out[j].meters
		}
		return out[i].copro.Name < out[j].copro.Name
	})
	return out
}

type nearby struct {
	copro  *Copro
	meters float64
}

var (
	catalogOnce  sync.Once
	catalogCache *Catalog
	catalogErr   error
)

// Load returns the singleton CDSR catalog, resolving the artifact from dir (the
// datadir) with a fallback to the embedded snapshot, parsed on first call. The
// dir from the first call wins for the process lifetime. A missing
// (non-embedded) artifact yields an empty catalog rather than an error.
func Load(dir string) (*Catalog, error) {
	catalogOnce.Do(func() {
		catalogCache, catalogErr = parse(dir)
	})
	return catalogCache, catalogErr
}

func parse(dir string) (*Catalog, error) {
	rc, err := set.Open(dir)
	if errors.Is(err, dataset.ErrUnavailable) {
		return &Catalog{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()

	raw, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	var copros []Copro
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &copros); err != nil {
			return nil, err
		}
	}
	return &Catalog{Copros: copros}, nil
}
