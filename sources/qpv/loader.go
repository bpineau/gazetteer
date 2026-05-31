package qpv

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
	"github.com/bpineau/gazetteer/helpers/communes"
	"github.com/bpineau/gazetteer/helpers/geodist"
	"github.com/bpineau/gazetteer/helpers/geopoly"
)

//go:embed data/qpv.json.gz
var embedFS embed.FS

// set binds the embedded contours to the datadir/refresh pipeline. Refresh
// downloads the upstream ANCT QPV 2024 contours ZIP and rebuilds the compact
// gzipped artifact via transform.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "qpv.json.gz"},
	Raw:       []dataset.File{{Name: rawName, URL: rawURL}},
	Transform: transform,
	Validate:  validate,
}

// qpvRow is one QPV in the compact embedded artifact: its identity, hosting
// commune INSEE codes, and boundary geometry ([polygon][ring][vertex][lon,lat]).
type qpvRow struct {
	Code     string           `json:"code"`
	Label    string           `json:"label,omitempty"`
	INSEE    []string         `json:"insee,omitempty"`
	Polygons [][][][2]float64 `json:"g"`
}

// Meta carries the manifest metadata for the embedded artifact.
type Meta struct {
	Source      string `json:"source"`
	RowCountQPV int    `json:"row_count_qpv"`
	RowCountCom int    `json:"row_count_communes"`
	CRS         string `json:"crs"`
	Note        string `json:"note"`
}

// processed is the on-disk JSON shape of the gzipped artifact.
type processed struct {
	Meta Meta     `json:"meta"`
	QPVs []qpvRow `json:"qpvs"`
}

// poly is one resolvable QPV polygon: identity, hosting communes, geometry and
// a precomputed bbox.
type poly struct {
	code, label string
	insee       []string
	bbox        geopoly.BBox
	mp          geopoly.MultiPolygon
}

// Entry is one commune's QPV list (used by the commune-level fallback).
type Entry struct {
	Label string
	QPVs  []QPV
}

// Index is the in-memory QPV contour index: polygons for point-in-polygon, and
// a commune→QPVs map for the coordinate-less fallback.
type Index struct {
	Meta      Meta
	polys     []poly
	byCommune map[string]Entry
}

// hit is the result of a point-in-polygon resolve.
type hit struct {
	Code  string
	Label string
}

// resolvePoint returns the QPV whose polygon contains (lat, lon), or nil when
// the point is outside every QPV. The bbox pre-filter keeps the O(n) scan
// cheap. polys are kept in code-sorted order (see parse / NewIndexForTest), so
// the first cover wins deterministically on a shared boundary.
func (idx *Index) resolvePoint(lat, lon float64) *hit {
	if idx == nil {
		return nil
	}
	p := geopoly.Point{Lon: lon, Lat: lat}
	for i := range idx.polys {
		pl := &idx.polys[i]
		if pl.bbox.Contains(p) && pl.mp.Covers(p) {
			return &hit{Code: pl.code, Label: pl.label}
		}
	}
	return nil
}

// nearest returns the QPV with the smallest vertex distance to (lat, lon) and
// that distance in metres, considering only QPVs whose bbox-expanded reach
// could plausibly fall within maxMeters. Returns nil when none qualifies.
func (idx *Index) nearest(lat, lon, maxMeters float64) (*hit, float64) {
	if idx == nil {
		return nil, 0
	}
	best := maxMeters
	var bestHit *hit
	for i := range idx.polys {
		pl := &idx.polys[i]
		for _, polygon := range pl.mp {
			for _, ring := range polygon {
				for _, v := range ring {
					d := geodist.MetersBetween(lat, lon, v.Lat, v.Lon)
					if d < best {
						best = d
						bestHit = &hit{Code: pl.code, Label: pl.label}
					}
				}
			}
		}
	}
	if bestHit == nil {
		return nil, 0
	}
	return bestHit, best
}

// lookupCommune returns the commune's QPV entry; ok is false when the commune
// hosts no QPV.
func (idx *Index) lookupCommune(insee string) (Entry, bool) {
	if idx == nil {
		return Entry{}, false
	}
	insee = strings.TrimSpace(insee)
	if insee == "" {
		return Entry{}, false
	}
	e, ok := idx.byCommune[insee]
	return e, ok
}

// PolygonCount reports the number of QPV polygons in the index.
func (idx *Index) PolygonCount() int {
	if idx == nil {
		return 0
	}
	return len(idx.polys)
}

// CommuneCount reports the number of communes hosting at least one QPV.
func (idx *Index) CommuneCount() int {
	if idx == nil {
		return 0
	}
	return len(idx.byCommune)
}

// FeatureForTest is one in-memory QPV used by NewIndexForTest to build an Index
// without a dataset artifact.
type FeatureForTest struct {
	Code     string
	Label    string
	INSEE    []string
	Polygons geopoly.MultiPolygon
}

// NewIndexForTest builds an Index from in-memory QPV polygons. Test seam:
// production callers use Load. Commune membership folds arrondissements, same
// as the real build.
func NewIndexForTest(feats []FeatureForTest) *Index {
	idx := &Index{byCommune: map[string]Entry{}}
	for _, f := range feats {
		pl := poly{code: f.Code, label: f.Label, insee: f.INSEE, mp: f.Polygons}
		pl.bbox = pl.mp.Bound()
		idx.polys = append(idx.polys, pl)
		addCommunes(idx.byCommune, f.INSEE, f.Code, f.Label)
	}
	idx.Meta = Meta{RowCountQPV: len(idx.polys), RowCountCom: len(idx.byCommune)}
	return idx
}

// addCommunes inserts a QPV into the commune→QPVs index under every hosting
// INSEE, folding arrondissements to the parent commune (Paris/Lyon/Marseille).
func addCommunes(m map[string]Entry, insees []string, code, label string) {
	for _, raw := range insees {
		ins := communes.FoldArrondissement(strings.TrimSpace(raw))
		if ins == "" {
			continue
		}
		e := m[ins]
		e.QPVs = append(e.QPVs, QPV{Code: code, Label: label})
		m[ins] = e
	}
}

var (
	indexOnce  sync.Once
	indexCache *Index
	indexErr   error
)

// Load returns the singleton contour index, resolving the artifact from dir
// (the datadir) with a fallback to the embedded snapshot, parsed on first call.
// The dir from the first call wins for the process lifetime. A missing
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
		return &Index{byCommune: map[string]Entry{}}, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	return parseIndex(rc)
}

// parseIndex decodes the gzipped JSON artifact into an Index, building both the
// polygon list (point-in-polygon) and the commune→QPVs map (fallback).
func parseIndex(r io.Reader) (*Index, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("qpv: gunzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	var p processed
	if err := json.NewDecoder(gz).Decode(&p); err != nil {
		return nil, fmt.Errorf("qpv: parse json: %w", err)
	}
	idx := &Index{Meta: p.Meta, polys: make([]poly, 0, len(p.QPVs)), byCommune: map[string]Entry{}}
	for _, r := range p.QPVs {
		pl := poly{code: r.Code, label: r.Label, insee: r.INSEE, mp: toMultiPolygon(r.Polygons)}
		pl.bbox = pl.mp.Bound()
		idx.polys = append(idx.polys, pl)
		addCommunes(idx.byCommune, r.INSEE, r.Code, r.Label)
	}
	return idx, nil
}

// toMultiPolygon converts the compact [polygon][ring][vertex][lon,lat] shape
// into a geopoly.MultiPolygon.
func toMultiPolygon(polys [][][][2]float64) geopoly.MultiPolygon {
	mp := make(geopoly.MultiPolygon, 0, len(polys))
	for _, polygon := range polys {
		gp := make(geopoly.Polygon, 0, len(polygon))
		for _, ring := range polygon {
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
