package nuisances

import (
	"embed"
	"io"
	"math"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/geodist"
)

//go:embed data/nuisances.json.gz
var embedFS embed.FS

// set binds the embedded grid snapshot to its Opendatasoft upstream.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "nuisances.json.gz"},
	Raw:       []dataset.File{{Name: rawName, URL: rawURL}},
	Transform: transform,
	Validate:  validate,
}

// cell is one 500 m grid cell: its centre and nuisance reading.
type cell struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Nuis int     `json:"nuis"`
	PNE  bool    `json:"pne,omitempty"`
}

// processed is the embedded artifact: the full IDF grid.
type processed struct {
	Cells []cell `json:"cells"`
}

// bucketDeg is the spatial-hash bucket size in degrees (~1 km). The 3×3-bucket
// search in nearest is correct only while one bucket is wider than the search
// radius: a bucket spans ≥ ~725 m of longitude even at the grid's northern edge
// (lat 49.24°) and ~1.1 km of latitude, both comfortably above MaxCellMeters
// (400 m), so the nearest centre is always within ±1 bucket. If MaxCellMeters
// ever exceeds that narrowest bucket width, widen the neighbourhood accordingly.
const bucketDeg = 0.01

type bucketKey struct{ a, b int }

func keyFor(lat, lon float64) bucketKey {
	return bucketKey{int(math.Floor(lat / bucketDeg)), int(math.Floor(lon / bucketDeg))}
}

// Index is the spatial lookup built from the grid.
type Index struct {
	buckets map[bucketKey][]cell
	n       int
}

// Count reports the number of cells in the grid.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return idx.n
}

// nearest returns the grid cell whose centre is closest to (lat, lon) within
// maxM metres, and that distance. ok is false when the point is outside the
// grid (no cell within maxM). See bucketDeg for the ±1-bucket correctness
// invariant.
func (idx *Index) nearest(lat, lon, maxM float64) (cell, float64, bool) {
	if idx == nil {
		return cell{}, 0, false
	}
	k := keyFor(lat, lon)
	best := math.MaxFloat64
	var bestCell cell
	found := false
	for da := -1; da <= 1; da++ {
		for db := -1; db <= 1; db++ {
			for _, c := range idx.buckets[bucketKey{k.a + da, k.b + db}] {
				d := geodist.MetersBetween(lat, lon, c.Lat, c.Lon)
				if d < best {
					best, bestCell, found = d, c, true
				}
			}
		}
	}
	if !found || best > maxM {
		return cell{}, 0, false
	}
	return bestCell, best, true
}

var lazyIndex dataset.Lazy[Index]

// Load returns the singleton grid index, resolving the artifact from dir (the
// datadir) with a fallback to the embedded snapshot, parsed on first call. A
// missing (non-embedded) artifact yields an empty index rather than an error.
func Load(dir string) (*Index, error) {
	return lazyIndex.Load(set, dir, parseIndex)
}

// parseIndex decodes the gzipped JSON artifact and buckets the cells for
// constant-time neighbourhood lookups.
func parseIndex(r io.Reader) (*Index, error) {
	p, err := dataset.ReadGzJSON[processed](r, Name)
	if err != nil {
		return nil, err
	}
	idx := &Index{buckets: make(map[bucketKey][]cell), n: len(p.Cells)}
	for _, c := range p.Cells {
		k := keyFor(c.Lat, c.Lon)
		idx.buckets[k] = append(idx.buckets[k], c)
	}
	return idx, nil
}
