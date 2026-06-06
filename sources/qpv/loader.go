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
	"github.com/bpineau/gazetteer/helpers/geoindex"
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
	Polygons geoindex.Compact `json:"g"`
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

// Entry is one commune's QPV list (used by the commune-level fallback).
type Entry struct {
	Label string
	QPVs  []QPV
}

// Index is the in-memory QPV contour index: a geoindex of QPV polygons for
// point-in-polygon (payload is the QPV identity), and a commune→QPVs map for
// the coordinate-less fallback.
type Index struct {
	Meta      Meta
	geo       *geoindex.Index[QPV]
	byCommune map[string]Entry
}

// resolvePoint returns the QPV whose polygon contains (lat, lon), or nil when
// the point is outside every QPV. The bbox pre-filter keeps the O(n) scan
// cheap. Features are kept in code-sorted order (see parse / NewIndexForTest),
// so the first cover wins deterministically on a shared boundary.
func (idx *Index) resolvePoint(lat, lon float64) *QPV {
	if idx == nil {
		return nil
	}
	if h, ok := idx.geo.Resolve(lat, lon); ok {
		return &h
	}
	return nil
}

// nearest returns the QPV with the smallest vertex distance to (lat, lon) and
// that distance in metres, considering only QPVs whose nearest vertex falls
// within maxMeters. Returns nil when none qualifies.
func (idx *Index) nearest(lat, lon, maxMeters float64) (*QPV, float64) {
	if idx == nil {
		return nil, 0
	}
	if h, dist, ok := idx.geo.Nearest(lat, lon, maxMeters); ok {
		return &h, dist
	}
	return nil, 0
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

// HasQPV reports whether the commune identified by its INSEE code hosts at
// least one QPV. This is a coordinate-free commune-level test; it does not
// check whether a specific point falls inside a QPV polygon.
func (idx *Index) HasQPV(insee string) bool {
	_, ok := idx.lookupCommune(insee)
	return ok
}

// PolygonCount reports the number of QPV polygons in the index.
func (idx *Index) PolygonCount() int {
	if idx == nil {
		return 0
	}
	return idx.geo.Len()
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
	gfeats := make([]geoindex.Feature[QPV], 0, len(feats))
	for _, f := range feats {
		gfeats = append(gfeats, geoindex.NewFeature(QPV{Code: f.Code, Label: f.Label}, f.Polygons))
		addCommunes(idx.byCommune, f.INSEE, f.Code, f.Label)
	}
	idx.geo = geoindex.New(gfeats)
	idx.Meta = Meta{RowCountQPV: idx.geo.Len(), RowCountCom: len(idx.byCommune)}
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
	idx := &Index{Meta: p.Meta, byCommune: map[string]Entry{}}
	gfeats := make([]geoindex.Feature[QPV], 0, len(p.QPVs))
	for _, r := range p.QPVs {
		gfeats = append(gfeats, geoindex.NewFeature(QPV{Code: r.Code, Label: r.Label}, r.Polygons.MultiPolygon()))
		addCommunes(idx.byCommune, r.INSEE, r.Code, r.Label)
	}
	idx.geo = geoindex.New(gfeats)
	return idx, nil
}
