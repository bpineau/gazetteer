package iris

import (
	"compress/gzip"
	"embed"
	"encoding/json"
	"errors"
	"sync"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/geopoly"
)

//go:embed data/iris.json.gz
var embedFS embed.FS

// set binds the embedded contours to their Opendatasoft upstream.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "iris.json.gz"},
	Raw:       []dataset.File{{Name: rawName, URL: rawURL}},
	Transform: transform,
	Validate:  validate,
}

// irisRow is one IRIS in the compact embedded artifact: its identity and
// boundary geometry ([polygon][ring][vertex][lon,lat]).
type irisRow struct {
	Code     string           `json:"code"`
	Nom      string           `json:"nom"`
	Typ      string           `json:"typ"`
	Polygons [][][][2]float64 `json:"g"`
}

type processed struct {
	Iris []irisRow `json:"iris"`
}

// area is one resolvable IRIS: identity, geometry and a precomputed bbox.
type area struct {
	code, nom, typ string
	bbox           geopoly.BBox
	mp             geopoly.MultiPolygon
}

// Index is the in-memory IRIS contour index.
type Index struct {
	areas  []area
	byCode map[string]area
}

// resolve returns the IRIS whose polygon contains (lat, lon). ok is false when
// the point is outside the covered perimeter.
//
// areas are stored in code-sorted order (see parse), so the first covering
// polygon wins deterministically when a point lands on a boundary shared by two
// IRIS — a negligible case given coordinates are real geocodes and boundaries
// are rounded to ~1 m. The bbox pre-filter keeps the O(n) scan cheap (one call
// per normalized address).
func (idx *Index) resolve(lat, lon float64) (code, nom, typ string, ok bool) {
	if idx == nil {
		return "", "", "", false
	}
	p := geopoly.Point{Lon: lon, Lat: lat}
	for i := range idx.areas {
		a := &idx.areas[i]
		if a.bbox.Contains(p) && a.mp.Covers(p) {
			return a.code, a.nom, a.typ, true
		}
	}
	return "", "", "", false
}

// lookupCode returns the name/type for a known IRIS code (used when a Listing
// already carries a resolved IRIS, to avoid re-running point-in-polygon).
func (idx *Index) lookupCode(code string) (nom, typ string, ok bool) {
	if idx == nil {
		return "", "", false
	}
	a, ok := idx.byCode[code]
	return a.nom, a.typ, ok
}

// Count reports the number of IRIS in the perimeter.
func (idx *Index) Count() int {
	if idx == nil {
		return 0
	}
	return len(idx.areas)
}

var (
	indexOnce  sync.Once
	indexCache *Index
	indexErr   error
)

// Load returns the singleton contour index, resolving the artifact from dir (the
// datadir) with a fallback to the embedded snapshot, parsed on first call. The
// dir from the first call wins for the process lifetime. A missing
// (non-embedded) artifact yields an empty index rather than an error.
func Load(dir string) (*Index, error) {
	indexOnce.Do(func() {
		indexCache, indexErr = parse(dir)
	})
	return indexCache, indexErr
}

func parse(dir string) (*Index, error) {
	rc, err := set.Open(dir)
	if errors.Is(err, dataset.ErrUnavailable) {
		return &Index{byCode: map[string]area{}}, nil
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
	// The artifact is code-sorted (see transform); appending in order keeps
	// areas sorted, which resolve relies on for deterministic first-cover
	// tie-breaks. IRIS codes are unique upstream, so byCode and areas agree.
	idx := &Index{areas: make([]area, 0, len(p.Iris)), byCode: make(map[string]area, len(p.Iris))}
	for _, r := range p.Iris {
		a := area{code: r.Code, nom: r.Nom, typ: r.Typ, mp: toMultiPolygon(r.Polygons)}
		a.bbox = a.mp.Bound()
		idx.areas = append(idx.areas, a)
		idx.byCode[r.Code] = a
	}
	return idx, nil
}

// toMultiPolygon converts the compact [polygon][ring][vertex][lon,lat] shape
// into a geopoly.MultiPolygon.
func toMultiPolygon(polys [][][][2]float64) geopoly.MultiPolygon {
	mp := make(geopoly.MultiPolygon, 0, len(polys))
	for _, poly := range polys {
		gp := make(geopoly.Polygon, 0, len(poly))
		for _, ring := range poly {
			gr := make(geopoly.Ring, 0, len(ring))
			for _, v := range ring {
				gr = append(gr, geopoly.Point{Lon: v[0], Lat: v[1]})
			}
			gp = append(gp, gr)
		}
		mp = append(mp, gp)
	}
	return mp
}
