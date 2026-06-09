package gpe

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/geodist"
)

//go:embed data/gpe_stations.json
var embedFS embed.FS

// set binds the embedded GPE station catalog to the datadir/refresh pipeline.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "gpe_stations.json"},
	Raw:       []dataset.File{{Name: rawName, URL: rawURL}},
	Transform: transform,
	Validate:  validate,
}

// MaxRelevantMeters caps the nearest-station search: beyond this a future GPE
// station is not a meaningful local driver, and the Result is empty.
const MaxRelevantMeters = 6000

// stationRec is one embedded station record.
type stationRec struct {
	Code string  `json:"code"`
	Name string  `json:"name"`
	Line string  `json:"line"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

// Meta carries the manifest metadata for the catalog.
type Meta struct {
	Source       string `json:"source"`
	DownloadedAt string `json:"downloaded_at"`
	StationCount int    `json:"station_count"`
	Note         string `json:"note"`
}

// Index is the embedded GPE station catalog.
type Index struct {
	Meta     Meta         `json:"meta"`
	Stations []stationRec `json:"stations"`
}

var lazyIndex dataset.Lazy[Index]

// Load returns the singleton catalog, resolving the processed artifact from
// dir (the datadir) with a fallback to the embedded copy. The dir from the
// first call wins for the process lifetime (subsequent calls ignore dir). A
// dataset that is neither in the datadir nor embedded yields an empty catalog
// (graceful degradation), not an error.
func Load(dir string) (*Index, error) {
	return lazyIndex.Load(set, dir, parseIndex)
}

// parseIndex decodes the JSON catalog.
func parseIndex(r io.Reader) (*Index, error) {
	body, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("gpe: read catalog: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(body, &idx); err != nil {
		return nil, fmt.Errorf("gpe: parse catalog: %w", err)
	}
	return &idx, nil
}

// Count returns the number of stations in the catalog.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.Stations)
}

// nearest computes the closest station to (lat, lon) plus the counts within
// the 1.5 km / 3 km radii. ok is false when the catalog is empty or the
// nearest station is beyond MaxRelevantMeters. The returned Station's
// DistanceM is rounded to the metre.
func (idx *Index) nearest(lat, lon float64) (s Station, within1500, within3000 int, ok bool) {
	if idx == nil || len(idx.Stations) == 0 {
		return Station{}, 0, 0, false
	}
	bestDist := -1.0
	var best stationRec
	for _, st := range idx.Stations {
		d := geodist.MetersBetween(lat, lon, st.Lat, st.Lon)
		if d <= 1500 {
			within1500++
		}
		if d <= 3000 {
			within3000++
		}
		if bestDist < 0 || d < bestDist {
			bestDist, best = d, st
		}
	}
	if bestDist > MaxRelevantMeters {
		return Station{}, within1500, within3000, false
	}
	return Station{Code: best.Code, Name: best.Name, Line: best.Line, DistanceM: int(bestDist + 0.5)}, within1500, within3000, true
}

// sortStations orders a station slice by code for deterministic output.
func sortStations(ss []stationRec) {
	sort.Slice(ss, func(i, j int) bool { return ss[i].Code < ss[j].Code })
}
