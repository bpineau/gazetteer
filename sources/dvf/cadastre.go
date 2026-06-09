package dvf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"

	"github.com/bpineau/gazetteer/helpers/geopoly"
	"github.com/bpineau/gazetteer/helpers/httpx"
)

// CadastreSectionsBaseURL is the cadastre-etalab JSON endpoint that lists
// every cadastral section feature for a given INSEE commune. Exposed as a
// var for tests.
//
// Schema (excerpt):
//
//	{
//	  "type": "FeatureCollection",
//	  "features": [
//	    {"id": "930720000A",
//	     "properties": {"commune": "93072", "prefixe": "000", "code": "A"}},
//	    ...
//	  ]
//	}
//
// This is the same endpoint that app.dvf.etalab.gouv.fr's webapp uses to
// populate its sections layer (cf. js/data.js → getCadastreLayer). Using
// it here lets us know the *exact* list of sections that exist for a
// commune without brute-forcing the 26×26 namespace — critical for
// communes whose section codes are 1-letter (e.g. Stains 93072 = A,B,C…)
// which the existing 000AA..000ZZ walker can never discover.
var CadastreSectionsBaseURL = "https://cadastre.data.gouv.fr/bundler/cadastre-etalab/communes"

// ErrCadastreCommuneNotFound is returned when the cadastre-etalab API has
// no sections file for the given INSEE (typically the umbrella codes
// `75056`, `13055`, `69123` of Paris/Marseille/Lyon — our pipeline never
// resolves to those, but defensive nonetheless).
var ErrCadastreCommuneNotFound = errors.New("dvf: cadastre commune not found")

// cadastreFeatureCollection is the GeoJSON envelope returned by
// cadastre.data.gouv.fr. We only model the few fields we need.
type cadastreFeatureCollection struct {
	Features []cadastreFeature `json:"features"`
}

type cadastreFeature struct {
	Properties cadastreSectionProps `json:"properties"`
	Geometry   cadastreGeometry     `json:"geometry"`
}

type cadastreSectionProps struct {
	Commune string `json:"commune"`
	Prefixe string `json:"prefixe"`
	Code    string `json:"code"`
}

// cadastreGeometry holds only the raw GeoJSON coordinates; we never need
// the (Multi)Polygon structure, just every vertex to derive a bounding box.
type cadastreGeometry struct {
	Coordinates json.RawMessage `json:"coordinates"`
}

// SectionGeo pairs a DVF section code with the bounding box of its cadastral
// geometry. The box lets a caller cheaply prefilter the commune's sections to
// those near a point before fetching their (much heavier) mutations.
type SectionGeo struct {
	Code string
	Box  geopoly.BBox
}

// FetchCadastreSections returns the list of cadastral section codes for
// commune `insee`, formatted for the DVF Etalab API path component (5
// chars total: `prefixe` (always "000") + left-padded code, e.g. "000AA"
// or "0000A"). On a 404 from the cadastre API, returns
// ErrCadastreCommuneNotFound.
//
// The DVF API path is `/api/mutations3/{insee}/{section}` where section
// is exactly the value `idSectionToCode` extracts in the DVF webapp js:
// `featureID.substr(5, 5)`. A featureID like "930720000A" yields "0000A";
// "751190000AA" yields "000AA". So the rule is:
//
//	dvfSection = "0" * (5 - len(prefixe) - len(code)) + prefixe + code
//
// Equivalent to right-aligning `prefixe + code` in a 5-char field.
func FetchCadastreSections(ctx context.Context, http *httpx.Client, insee string) ([]string, error) {
	if http == nil {
		return nil, errors.New("dvf: nil http client")
	}
	if insee == "" {
		return nil, errors.New("dvf: empty insee")
	}
	u := fmt.Sprintf("%s/%s/geojson/sections",
		CadastreSectionsBaseURL,
		url.PathEscape(insee),
	)
	body, _, err := http.GetBytes(ctx, u, nil)
	if err != nil {
		if herr, ok := errors.AsType[*httpx.ErrHTTP](err); ok && herr.Status == 404 {
			return nil, ErrCadastreCommuneNotFound
		}
		return nil, fmt.Errorf("dvf: cadastre GET %s: %w", insee, err)
	}
	var fc cadastreFeatureCollection
	if err := json.Unmarshal(body, &fc); err != nil {
		return nil, fmt.Errorf("dvf: cadastre decode %s: %w", insee, err)
	}
	out := make([]string, 0, len(fc.Features))
	seen := make(map[string]struct{}, len(fc.Features))
	for _, f := range fc.Features {
		// Defensive: skip features that don't actually belong to this
		// commune. Cadastre data is occasionally noisy near commune
		// borders.
		if f.Properties.Commune != insee {
			continue
		}
		dvfCode := dvfSectionCode(f.Properties.Prefixe, f.Properties.Code)
		if dvfCode == "" {
			continue
		}
		if _, dup := seen[dvfCode]; dup {
			continue
		}
		seen[dvfCode] = struct{}{}
		out = append(out, dvfCode)
	}
	return out, nil
}

// FetchCadastreSectionGeos is FetchCadastreSections plus each section's
// bounding box, parsed from the same cadastre-etalab GeoJSON. Sections whose
// geometry is empty/unparseable are returned with their (inverted-infinity)
// zero box — callers should treat that as "everywhere" or skip it, never as a
// tight box at (0,0). On a 404, returns ErrCadastreCommuneNotFound.
//
// Used by the DVF address_radius tier to fetch only the handful of sections
// whose box falls within the disk radius, instead of every section in a dense
// commune (a Paris arrondissement has ~50).
func FetchCadastreSectionGeos(ctx context.Context, http *httpx.Client, insee string) ([]SectionGeo, error) {
	if http == nil {
		return nil, errors.New("dvf: nil http client")
	}
	if insee == "" {
		return nil, errors.New("dvf: empty insee")
	}
	u := fmt.Sprintf("%s/%s/geojson/sections",
		CadastreSectionsBaseURL,
		url.PathEscape(insee),
	)
	body, _, err := http.GetBytes(ctx, u, nil)
	if err != nil {
		if herr, ok := errors.AsType[*httpx.ErrHTTP](err); ok && herr.Status == 404 {
			return nil, ErrCadastreCommuneNotFound
		}
		return nil, fmt.Errorf("dvf: cadastre GET %s: %w", insee, err)
	}
	var fc cadastreFeatureCollection
	if err := json.Unmarshal(body, &fc); err != nil {
		return nil, fmt.Errorf("dvf: cadastre decode %s: %w", insee, err)
	}
	// Union boxes by section code: a section is occasionally split across
	// several features near commune borders.
	idx := make(map[string]int, len(fc.Features))
	out := make([]SectionGeo, 0, len(fc.Features))
	for _, f := range fc.Features {
		if f.Properties.Commune != insee {
			continue
		}
		dvfCode := dvfSectionCode(f.Properties.Prefixe, f.Properties.Code)
		if dvfCode == "" {
			continue
		}
		box := emptyBBox()
		accumulateBBox(f.Geometry.Coordinates, &box)
		if i, ok := idx[dvfCode]; ok {
			out[i].Box = unionBBox(out[i].Box, box)
			continue
		}
		idx[dvfCode] = len(out)
		out = append(out, SectionGeo{Code: dvfCode, Box: box})
	}
	return out, nil
}

// emptyBBox is the inverted-infinity box: every Min is +Inf, every Max -Inf,
// so accumulateBBox widens it on the first vertex and Contains stays false
// until then.
func emptyBBox() geopoly.BBox {
	return geopoly.BBox{
		MinLon: math.Inf(1), MinLat: math.Inf(1),
		MaxLon: math.Inf(-1), MaxLat: math.Inf(-1),
	}
}

// unionBBox returns the smallest box covering both inputs.
func unionBBox(a, b geopoly.BBox) geopoly.BBox {
	return geopoly.BBox{
		MinLon: math.Min(a.MinLon, b.MinLon),
		MinLat: math.Min(a.MinLat, b.MinLat),
		MaxLon: math.Max(a.MaxLon, b.MaxLon),
		MaxLat: math.Max(a.MaxLat, b.MaxLat),
	}
}

// accumulateBBox walks a GeoJSON coordinates value of any nesting depth
// (Polygon = 3 levels, MultiPolygon = 4) and widens b to include every
// [lon, lat] vertex. The raw value is parsed ONCE into the generic JSON
// tree and then walked: a leaf coordinate pair like [2.35, 48.86] (or
// [lon, lat, alt]) is recognised by its first element being a number.
// The previous implementation trial-decoded a flat []float64 at every
// nesting level, re-parsing each subtree twice per level.
func accumulateBBox(raw json.RawMessage, b *geopoly.BBox) {
	if len(raw) == 0 {
		return
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return
	}
	accumulateBBoxTree(v, b)
}

// accumulateBBoxTree recursively widens b over a decoded GeoJSON
// coordinates tree ([]any nesting with float64 leaves). Non-array,
// non-pair shapes are ignored — same tolerance as the trial-decode
// approach it replaces.
func accumulateBBoxTree(v any, b *geopoly.BBox) {
	arr, ok := v.([]any)
	if !ok || len(arr) == 0 {
		return
	}
	if lon, ok := arr[0].(float64); ok {
		// Leaf [lon, lat, ...] coordinate pair.
		if len(arr) < 2 {
			return
		}
		lat, ok := arr[1].(float64)
		if !ok {
			return
		}
		b.MinLon = math.Min(b.MinLon, lon)
		b.MinLat = math.Min(b.MinLat, lat)
		b.MaxLon = math.Max(b.MaxLon, lon)
		b.MaxLat = math.Max(b.MaxLat, lat)
		return
	}
	for _, e := range arr {
		accumulateBBoxTree(e, b)
	}
}

// dvfSectionCode formats a (prefixe, code) pair as the 5-char string the
// DVF Etalab API expects. Returns "" for malformed inputs.
//
// Examples (real-world):
//
//	("000", "AA") → "000AA"   (typical Paris arrondissement)
//	("000", "A")  → "0000A"   (typical small commune, e.g. Stains 93072)
//	("050", "AB") → "050AB"   (rare — non-default prefix)
func dvfSectionCode(prefixe, code string) string {
	if prefixe == "" || code == "" {
		return ""
	}
	combined := prefixe + code
	if len(combined) >= 5 {
		// Already wide enough — the API path uses the last 5 chars of
		// the cadastre id (cf. dvf-app js/index.js idSectionToCode).
		return combined[len(combined)-5:]
	}
	// Left-pad with '0' to reach 5 chars.
	pad := 5 - len(combined)
	zeros := []byte{'0', '0', '0', '0', '0'}
	return string(zeros[:pad]) + combined
}
