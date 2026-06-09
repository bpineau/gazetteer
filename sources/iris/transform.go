package iris

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/geoindex"
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
		polys, err := geoindex.DecodeGeoJSONGeometry(f.Geometry.Type, f.Geometry.Coordinates, coordDecimals)
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

	return dataset.WriteGzJSON(dst, processed{Iris: out})
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
