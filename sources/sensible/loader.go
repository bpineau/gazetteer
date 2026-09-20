package sensible

import (
	"embed"
	"io"
	"math"
	"slices"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/communes"
	"github.com/bpineau/gazetteer/helpers/geodist"
	"github.com/bpineau/gazetteer/helpers/geoindex"
	"github.com/bpineau/gazetteer/helpers/geopoly"
)

//go:embed data/sensible_zones.json.gz
var embedFS embed.FS

// set binds the embedded QRR contours to the datadir/refresh pipeline.
// Refresh downloads the upstream ministère de l'Intérieur shapefile ZIP from
// data.gouv.fr and rebuilds the compact gzipped artifact via transform.
var set = dataset.Set{
	Source:    Name,
	Version:   Version,
	Embed:     embedFS,
	Processed: dataset.File{Name: "sensible_zones.json.gz"},
	Raw:       []dataset.File{{Name: rawName, URL: rawURL}},
	Transform: transform,
	Validate:  validate,
}

// zoneRow is one QRR in the compact embedded artifact: its identity and
// boundary geometry ([polygon][ring][vertex][lon,lat]).
type zoneRow struct {
	Name    string           `json:"name"`
	Dep     string           `json:"dep"`
	Service string           `json:"service,omitempty"`
	Vague   int              `json:"vague,omitempty"`
	Code    string           `json:"code,omitempty"`
	G       geoindex.Compact `json:"g"`
}

// Meta carries the manifest metadata for the embedded artifact.
type Meta struct {
	Source    string `json:"source"`
	ZoneCount int    `json:"zone_count"`
	CRS       string `json:"crs"`
	Note      string `json:"note"`
}

// processed is the on-disk JSON shape of the gzipped artifact.
type processed struct {
	Meta  Meta      `json:"meta"`
	Zones []zoneRow `json:"zones"`
}

// feature is one in-memory zone: its identity, geometry and bounding box.
// A plain slice (not a geoindex.Index) because this source reports EVERY
// containing/nearby zone, not just the first cover / single nearest.
type feature struct {
	code string // QRR code, the zoneCommunes key
	zone Zone   // DistanceM left 0; filled per-query
	mp   geopoly.MultiPolygon
	bbox geopoly.BBox
}

// Index is the in-memory sensitive-zone index: the QRR polygon features plus
// the curated circle overlay.
type Index struct {
	Meta  Meta
	feats []feature
}

var lazyIndex dataset.Lazy[Index]

// Load returns the singleton index, resolving the artifact from dir (the
// datadir) with a fallback to the embedded snapshot, parsed on first call.
// The dir from the first call wins for the process lifetime. A missing
// (non-embedded) artifact yields an empty index rather than an error.
func Load(dir string) (*Index, error) {
	return lazyIndex.Load(set, dir, parseIndex)
}

// parseIndex decodes the gzipped JSON artifact into an Index.
func parseIndex(r io.Reader) (*Index, error) {
	p, err := dataset.ReadGzJSON[processed](r, Name)
	if err != nil {
		return nil, err
	}
	idx := &Index{Meta: p.Meta, feats: make([]feature, 0, len(p.Zones))}
	for _, z := range p.Zones {
		mp := z.G.MultiPolygon()
		idx.feats = append(idx.feats, feature{
			code: z.Code,
			zone: Zone{Name: z.Name, Kind: KindQRR, Dep: z.Dep, Vague: z.Vague},
			mp:   mp,
			bbox: mp.Bound(),
		})
	}
	return idx, nil
}

// NewIndexForTest builds an Index from in-memory zones. Test seam: production
// callers use Load.
func NewIndexForTest(zones map[string]geopoly.MultiPolygon) *Index {
	idx := &Index{}
	for name, mp := range zones {
		idx.feats = append(idx.feats, feature{
			zone: Zone{Name: name, Kind: KindQRR},
			mp:   mp,
			bbox: mp.Bound(),
		})
	}
	idx.Meta = Meta{ZoneCount: len(idx.feats)}
	return idx
}

// ZoneCount reports the number of QRR polygons in the index (curated entries
// excluded).
func (idx *Index) ZoneCount() int {
	if idx == nil {
		return 0
	}
	return len(idx.feats)
}

// resolve classifies (lat, lon) against every zone — QRR polygons then the
// curated circles — and returns the containing zones (DistanceM 0) and the
// zones whose boundary lies within NearbyMeters (DistanceM set). 66 zones,
// each bbox-prefiltered: the common far-from-everything case costs 66 box
// tests and nothing else.
func (idx *Index) resolve(lat, lon float64) (in, nearby []Zone) {
	if idx == nil {
		return nil, nil
	}
	p := geopoly.Point{Lon: lon, Lat: lat}
	for _, f := range idx.feats {
		if !expand(f.bbox, NearbyMeters, lat).Contains(p) {
			continue
		}
		if f.mp.Covers(p) {
			z := f.zone
			in = append(in, z)
			continue
		}
		if d, ok := boundaryDistanceM(f.mp, lat, lon, NearbyMeters); ok {
			z := f.zone
			z.DistanceM = int(d + 0.5)
			nearby = append(nearby, z)
		}
	}
	for _, c := range curatedZones {
		d := geodist.MetersBetween(lat, lon, c.Lat, c.Lon)
		z := Zone{Name: c.Name, Kind: c.Kind, Dep: c.Dep, Note: c.Note}
		switch {
		case d <= c.RadiusM:
			in = append(in, z)
		case d <= c.RadiusM+NearbyMeters:
			z.DistanceM = int(d - c.RadiusM + 0.5)
			nearby = append(nearby, z)
		}
	}
	return in, nearby
}

// ZonesForCommune is the commune-grain batch view: every zone (QRR polygon or
// curated circle) whose perimeter intersects the commune identified by its
// INSEE code. The mapping comes from zoneCommunes / curatedZone.INSEE (see
// communes.go for the construction); arrondissements fold to the parent
// commune. DistanceM is 0 on every returned Zone — at this grain the question
// is "does the commune host such a zone?", not "how far is this address?".
// Intended for batch screens (one row per commune) alongside qpv.HasQPV.
func (idx *Index) ZonesForCommune(insee string) []Zone {
	if idx == nil {
		return nil
	}
	insee = communes.FoldArrondissement(strings.TrimSpace(insee))
	if insee == "" {
		return nil
	}
	var out []Zone
	for _, f := range idx.feats {
		if slices.Contains(zoneCommunes[f.code], insee) {
			out = append(out, f.zone)
		}
	}
	for _, c := range curatedZones {
		if slices.Contains(c.INSEE, insee) {
			out = append(out, Zone{Name: c.Name, Kind: c.Kind, Dep: c.Dep, Note: c.Note})
		}
	}
	return out
}

// expand grows a bbox by m metres on every side (planar approximation at the
// query latitude — same convention as geoindex's prefilter).
func expand(b geopoly.BBox, m, atLat float64) geopoly.BBox {
	const mPerDegLat = 111_320.0
	dLat := m / mPerDegLat
	dLon := dLat
	if c := math.Cos(atLat * math.Pi / 180); c > 0.1 {
		dLon = dLat / c
	}
	return geopoly.BBox{
		MinLon: b.MinLon - dLon, MinLat: b.MinLat - dLat,
		MaxLon: b.MaxLon + dLon, MaxLat: b.MaxLat + dLat,
	}
}

// boundaryDistanceM returns the distance from (lat, lon) to the nearest EDGE
// of mp's boundary, ok=false when it exceeds maxMeters.
//
// It used to measure to the nearest VERTEX, on the assumption that "QRR rings
// are dense enough that the difference is a few tens of metres at most". That
// assumption is false for 7 of the 62 shipped zones: across 29 417 edges the
// median is 15.9 m but the longest is 2 234 m, and a point 50 m outside the
// Lunel/Mauguio perimeter sits on such an edge — it was reported as no zone
// nearby at all, where the doc promises "zones whose boundary lies within this
// distance".
func boundaryDistanceM(mp geopoly.MultiPolygon, lat, lon, maxMeters float64) (float64, bool) {
	best := mp.BoundaryDistanceM(lat, lon)
	if best > maxMeters {
		return 0, false
	}
	return best, true
}
