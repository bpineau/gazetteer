package iris

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"

	"github.com/bpineau/gazetteer/dataset"
)

// coordDecimals rounds contour coordinates to ~1 m — far finer than any IRIS
// boundary, and enough to keep the embedded artifact compact.
const coordDecimals = 5

// Raw input (datadir basename) and upstream URL. The region publishes the IRIS
// contours (WGS84) on its Opendatasoft portal; the GeoJSON export carries
// code_iris / nom_iris / typ_iris per feature.
const (
	rawName = "iris.raw.geojson"
	rawURL  = "https://data.iledefrance.fr/api/explore/v2.1/catalog/datasets/iris/exports/geojson"
)

// transform compacts the IRIS contours GeoJSON into the gzipped embedded
// artifact: one row per IRIS with its identity and rounded boundary geometry,
// every other property dropped.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	var fc struct {
		Features []struct {
			Properties struct {
				CodeIRIS string `json:"code_iris"`
				NomIRIS  string `json:"nom_iris"`
				TypIRIS  string `json:"typ_iris"`
			} `json:"properties"`
			Geometry struct {
				Type        string          `json:"type"`
				Coordinates json.RawMessage `json:"coordinates"`
			} `json:"geometry"`
		} `json:"features"`
	}
	if err := json.NewDecoder(dataset.BOMReader(rc)).Decode(&fc); err != nil {
		return fmt.Errorf("iris: decode geojson: %w", err)
	}

	out := make([]irisRow, 0, len(fc.Features))
	for _, f := range fc.Features {
		code := f.Properties.CodeIRIS
		if code == "" {
			continue
		}
		polys, err := decodeGeometry(f.Geometry.Type, f.Geometry.Coordinates)
		if err != nil {
			return fmt.Errorf("iris: %s: %w", code, err)
		}
		if len(polys) == 0 {
			continue
		}
		out = append(out, irisRow{Code: code, Nom: f.Properties.NomIRIS, Typ: f.Properties.TypIRIS, Polygons: polys})
	}
	if len(out) == 0 {
		return errors.New("iris: transform produced no features")
	}
	// Deterministic order for byte-stable output.
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })

	gz := gzip.NewWriter(dst)
	if err := json.NewEncoder(gz).Encode(processed{Iris: out}); err != nil {
		return err
	}
	return gz.Close()
}

// decodeGeometry normalises a GeoJSON Polygon or MultiPolygon into the
// [polygon][ring][vertex][lon,lat] shape, rounding coordinates.
func decodeGeometry(typ string, coords json.RawMessage) ([][][][2]float64, error) {
	switch typ {
	case "Polygon":
		var p [][][]float64
		if err := json.Unmarshal(coords, &p); err != nil {
			return nil, fmt.Errorf("polygon coords: %w", err)
		}
		return [][][][2]float64{roundRings(p)}, nil
	case "MultiPolygon":
		var mp [][][][]float64
		if err := json.Unmarshal(coords, &mp); err != nil {
			return nil, fmt.Errorf("multipolygon coords: %w", err)
		}
		out := make([][][][2]float64, 0, len(mp))
		for _, p := range mp {
			out = append(out, roundRings(p))
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported geometry type %q", typ)
	}
}

func roundRings(rings [][][]float64) [][][2]float64 {
	out := make([][][2]float64, 0, len(rings))
	for _, ring := range rings {
		rr := make([][2]float64, 0, len(ring))
		for _, v := range ring {
			if len(v) < 2 {
				continue
			}
			rr = append(rr, [2]float64{roundTo(v[0]), roundTo(v[1])})
		}
		out = append(out, rr)
	}
	return out
}

func roundTo(f float64) float64 {
	p := math.Pow(10, coordDecimals)
	return math.Round(f*p) / p
}

// validate gates a freshly-built artifact: it must gunzip, parse, and carry a
// plausible number of IRIS with geometry.
func validate(r io.Reader) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("iris: validate gunzip: %w", err)
	}
	defer func() { _ = gz.Close() }()
	var p processed
	if err := json.NewDecoder(gz).Decode(&p); err != nil {
		return fmt.Errorf("iris: validate decode: %w", err)
	}
	if len(p.Iris) < 4000 {
		return fmt.Errorf("iris: only %d IRIS, want ≥ 4000", len(p.Iris))
	}
	for _, r := range p.Iris {
		if r.Code == "" || len(r.Polygons) == 0 {
			return fmt.Errorf("iris: %q has no geometry", r.Code)
		}
	}
	return nil
}
