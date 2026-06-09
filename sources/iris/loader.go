package iris

import (
	"embed"
	"io"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/geoindex"
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
	Polygons geoindex.Compact `json:"g"`
}

type processed struct {
	Iris []irisRow `json:"iris"`
}

// irisID is the per-feature payload carried by the geoindex: an IRIS identity.
type irisID struct {
	code, nom, typ string
}

// Index is the in-memory IRIS contour index: a geoindex of IRIS polygons for
// point-in-polygon, plus a code→identity map for direct lookups.
type Index struct {
	geo    *geoindex.Index[irisID]
	byCode map[string]irisID
}

// resolve returns the IRIS whose polygon contains (lat, lon). ok is false when
// the point is outside the covered perimeter.
//
// Features are stored in code-sorted order (see parse), so the first covering
// polygon wins deterministically when a point lands on a boundary shared by two
// IRIS — a negligible case given coordinates are real geocodes and boundaries
// are rounded to ~1 m. The bbox pre-filter keeps the O(n) scan cheap (one call
// per normalized address).
func (idx *Index) resolve(lat, lon float64) (code, nom, typ string, ok bool) {
	if idx == nil {
		return "", "", "", false
	}
	id, ok := idx.geo.Resolve(lat, lon)
	if !ok {
		return "", "", "", false
	}
	return id.code, id.nom, id.typ, true
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
	return idx.geo.Len()
}

var lazyIndex dataset.Lazy[Index]

// Load returns the singleton contour index, resolving the artifact from dir (the
// datadir) with a fallback to the embedded snapshot, parsed on first call. The
// dir from the first call wins for the process lifetime. A missing
// (non-embedded) artifact yields an empty index rather than an error.
func Load(dir string) (*Index, error) {
	return lazyIndex.Load(set, dir, parseIndex)
}

// parseIndex decodes the gzipped JSON artifact and builds the contour index.
func parseIndex(r io.Reader) (*Index, error) {
	p, err := dataset.ReadGzJSON[processed](r, Name)
	if err != nil {
		return nil, err
	}
	// The artifact is code-sorted (see transform); building features in order
	// keeps them sorted, which resolve relies on for deterministic first-cover
	// tie-breaks. IRIS codes are unique upstream, so byCode and the geoindex
	// agree.
	idx := &Index{byCode: make(map[string]irisID, len(p.Iris))}
	feats := make([]geoindex.Feature[irisID], 0, len(p.Iris))
	for _, r := range p.Iris {
		id := irisID{code: r.Code, nom: r.Nom, typ: r.Typ}
		feats = append(feats, geoindex.NewFeature(id, r.Polygons.MultiPolygon()))
		idx.byCode[r.Code] = id
	}
	idx.geo = geoindex.New(feats)
	return idx, nil
}
