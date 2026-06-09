package qpv

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/geoindex"
)

// coordDecimals rounds contour coordinates to ~11 m — well within QPV boundary
// fidelity (city-block scale), and enough to keep the embedded artifact under
// ~1.5 MB gzipped.
const coordDecimals = 4

// rawName is the datadir filename for the upstream raw input — the ANCT QPV
// 2024 contours ZIP (it bundles several GeoJSON variants; we use the WGS84
// hexagonale + outre-mer one).
const rawName = "qpv_geo.zip"

// rawURL is the ANCT national QPV 2024 contours ZIP, published on the
// data.gouv.fr dataset slug
// quartiers-prioritaires-de-la-politique-de-la-ville-qpv. data.gouv mints a
// dated static path per revision; bump this when the ANCT republishes (the
// slug page lists the current resource).
const rawURL = "https://static.data.gouv.fr/resources/quartiers-prioritaires-de-la-politique-de-la-ville-qpv/20260115-204323/qpv-2024-geojson.zip"

// geojsonMemberMarker selects the WGS84 hexagonale + outre-mer GeoJSON member
// inside the ZIP. The other members are Lambert-93 / UTM (reprojection we
// avoid) or per-territory subsets.
const geojsonMemberMarker = "WGS84.geojson"

// metaSource is the provenance string recorded in the rebuilt artifact.
const metaSource = "data.gouv.fr ANCT — Quartiers Prioritaires Politique de la Ville (QPV 2024) contours"

// metaCRS records the coordinate system of the embedded geometry.
const metaCRS = "WGS84 (CRS84, lon/lat)"

// metaNote documents the artifact semantics + coverage limitation.
const metaNote = "QPV 2024 contours (decree 2023-1314, effective 1 January 2024). " +
	"Métropole + Outre-mer in WGS84. Point-in-polygon membership; commune index for the coordinate-less fallback."

// Upstream GeoJSON property keys (QP2024_*_WGS84.geojson). Each feature
// carries exactly one hosting commune (insee_com / lib_com) — the contours
// are published one feature per QPV-within-commune.
const (
	propCodeQP = "code_qp"   // QPV code, format "QNXXXNNL"
	propLibQP  = "lib_qp"    // QPV name
	propInsee  = "insee_com" // hosting commune INSEE (5-digit, zero-padded)
)

// feature is one decoded GeoJSON feature (only the fields we keep).
type feature struct {
	Properties map[string]json.RawMessage `json:"properties"`
	Geometry   struct {
		Type        string          `json:"type"`
		Coordinates json.RawMessage `json:"coordinates"`
	} `json:"geometry"`
}

// transform rebuilds the processed qpv artifact from the upstream contours ZIP.
// It opens the ZIP, picks the WGS84 GeoJSON member, extracts per-QPV {code,
// label, commune INSEEs, multipolygon}, rounds coordinates, and emits a compact
// gzipped JSON artifact (nested float arrays, not raw GeoJSON).
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	body, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("qpv: read raw zip: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return fmt.Errorf("qpv: open zip: %w", err)
	}

	geo, err := openGeoJSONMember(zr)
	if err != nil {
		return err
	}
	defer func() { _ = geo.Close() }()

	var fc struct {
		Features []feature `json:"features"`
	}
	if err := json.NewDecoder(dataset.BOMReader(geo)).Decode(&fc); err != nil {
		return fmt.Errorf("qpv: decode geojson: %w", err)
	}

	out := make([]qpvRow, 0, len(fc.Features))
	communeSet := map[string]struct{}{}
	for _, f := range fc.Features {
		code := strProp(f.Properties, propCodeQP)
		if code == "" {
			continue
		}
		polys, err := geoindex.DecodeGeoJSONGeometry(f.Geometry.Type, f.Geometry.Coordinates, coordDecimals)
		if err != nil {
			return fmt.Errorf("qpv: %s: %w", code, err)
		}
		if len(polys) == 0 {
			continue
		}
		insees := splitINSEE(strProp(f.Properties, propInsee))
		for _, ins := range insees {
			communeSet[ins] = struct{}{}
		}
		out = append(out, qpvRow{
			Code:     code,
			Label:    strProp(f.Properties, propLibQP),
			INSEE:    insees,
			Polygons: polys,
		})
	}
	if len(out) == 0 {
		return errors.New("qpv: transform produced no features")
	}
	// Deterministic order for byte-stable output (and first-cover tie-breaks).
	sort.Slice(out, func(i, j int) bool { return out[i].Code < out[j].Code })

	p := processed{
		Meta: Meta{
			Source:      metaSource,
			RowCountQPV: len(out),
			RowCountCom: len(communeSet),
			CRS:         metaCRS,
			Note:        metaNote,
		},
		QPVs: out,
	}
	return dataset.WriteGzJSON(dst, p)
}

// openGeoJSONMember returns a reader over the WGS84 GeoJSON member of the ZIP.
func openGeoJSONMember(zr *zip.Reader) (io.ReadCloser, error) {
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, geojsonMemberMarker) {
			return f.Open()
		}
	}
	return nil, fmt.Errorf("qpv: no %q member in zip", geojsonMemberMarker)
}

// strProp extracts a string property (decoded JSON string) from a feature.
func strProp(props map[string]json.RawMessage, key string) string {
	raw, ok := props[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// splitINSEE parses the insee_com property into a slice of zero-padded 5-digit
// INSEE codes. The WGS84 contours carry a single commune per feature, but a
// comma-separated form is tolerated defensively and each code zero-padded.
func splitINSEE(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		for len(p) < 5 {
			p = "0" + p
		}
		out = append(out, p)
	}
	return out
}

// validate gates a freshly-built artifact: it must gunzip, parse, and carry a
// plausible number of QPVs each with a code and geometry.
func validate(r io.Reader) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("qpv: validate gunzip: %w", err)
	}
	defer func() { _ = gz.Close() }()

	var p processed
	if err := json.NewDecoder(gz).Decode(&p); err != nil {
		return fmt.Errorf("qpv: validate decode: %w", err)
	}
	if len(p.QPVs) == 0 {
		return errors.New("qpv: validated artifact has no QPV polygons")
	}
	for i := range p.QPVs {
		if p.QPVs[i].Code == "" || len(p.QPVs[i].Polygons) == 0 {
			return fmt.Errorf("qpv: polygon %d has no code/geometry", i)
		}
	}
	return nil
}
