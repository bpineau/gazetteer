package sensible

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/geoindex"
	"github.com/bpineau/gazetteer/helpers/geopoly"
)

// coordDecimals rounds contour coordinates to ~1 m — far within the fidelity
// of police-perimeter boundaries, and keeps the embedded artifact compact
// (~150 KB gzipped).
const coordDecimals = 5

// rawName is the datadir filename for the upstream raw input — the ministère
// de l'Intérieur QRR contours, a zipped ESRI shapefile (WGS84).
const rawName = "qrr_contours.zip"

// rawURL is the "Quartiers de Reconquête Républicaine" aggregated contours on
// data.gouv.fr (dataset slug quartiers-de-reconquete-republicaine-1).
// data.gouv mints a dated static path per revision; bump this if the ministry
// republishes (unlikely: the QRR list is frozen since 2021).
const rawURL = "https://static.data.gouv.fr/resources/quartiers-de-reconquete-republicaine-1/20240212-104429/qrr-agreges.zip"

// metaSource is the provenance string recorded in the rebuilt artifact.
const metaSource = "data.gouv.fr ministère de l'Intérieur — Quartiers de Reconquête Républicaine (contours agrégés)"

// metaCRS records the coordinate system of the embedded geometry.
const metaCRS = "WGS84 (EPSG:4326, lon/lat)"

// metaNote documents the artifact semantics.
const metaNote = "62 QRR police-priority perimeters (vagues 2018/2019/2021 ; liste figée depuis 2021). " +
	"Point-in-polygon membership; the ORCOD-IN overlay lives in code (curated.go), not in this artifact."

// transform rebuilds the processed artifact from the upstream shapefile ZIP:
// it parses the .shp polygons + .dbf attributes, rounds coordinates, and
// writes a name-sorted gzipped JSON catalog.
func transform(_ context.Context, raw dataset.RawSet, dst io.Writer) error {
	rc, err := raw.Open(rawName)
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()

	body, err := io.ReadAll(rc)
	if err != nil {
		return fmt.Errorf("sensible: read raw zip: %w", err)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return fmt.Errorf("sensible: open raw zip: %w", err)
	}
	shp, err := zipMember(zr, ".shp")
	if err != nil {
		return err
	}
	dbf, err := zipMember(zr, ".dbf")
	if err != nil {
		return err
	}

	shapes, err := readSHPPolygons(shp)
	if err != nil {
		return err
	}
	attrs, err := readDBF(dbf)
	if err != nil {
		return err
	}
	if len(shapes) != len(attrs) {
		return fmt.Errorf("sensible: shp/dbf record mismatch: %d shapes vs %d attribute rows", len(shapes), len(attrs))
	}

	zones := make([]zoneRow, 0, len(shapes))
	for i, rings := range shapes {
		a := attrs[i]
		name := strings.TrimSpace(a["nom"])
		if name == "" || len(rings) == 0 {
			continue // a QRR without a name or a geometry is unusable
		}
		vague, _ := strconv.ParseFloat(strings.TrimSpace(a["vague"]), 64)
		// All rings of one shapefile record form ONE even-odd polygon (outer
		// rings union, nested rings subtract) — geopoly.Polygon semantics.
		poly := make(geopoly.Polygon, 0, len(rings))
		for _, ring := range rings {
			r := make(geopoly.Ring, 0, len(ring))
			for _, pt := range ring {
				r = append(r, geopoly.Point{Lon: pt[0], Lat: pt[1]})
			}
			poly = append(poly, r)
		}
		zones = append(zones, zoneRow{
			Name:    name,
			Dep:     strings.TrimSpace(a["dep"]),
			Service: strings.TrimSpace(a["service"]),
			Vague:   int(vague),
			Code:    strings.TrimSpace(a["code_qrr"]),
			G:       geoindex.RoundCompact(geoindex.FromMultiPolygon(geopoly.MultiPolygon{poly}), coordDecimals),
		})
	}
	if len(zones) == 0 {
		return errors.New("sensible: transform produced no zones")
	}
	sort.Slice(zones, func(i, j int) bool { return zones[i].Name < zones[j].Name })

	out := processed{
		Meta: Meta{
			Source:    metaSource,
			ZoneCount: len(zones),
			CRS:       metaCRS,
			Note:      metaNote,
		},
		Zones: zones,
	}
	zw := gzip.NewWriter(dst)
	if err := json.NewEncoder(zw).Encode(out); err != nil {
		return fmt.Errorf("sensible: encode artifact: %w", err)
	}
	return zw.Close()
}

// validate gates a rebuilt artifact before it replaces the previous one: it
// must parse, carry a plausible zone count (the upstream has 62), and every
// zone must have a usable geometry.
func validate(r io.Reader) error {
	p, err := dataset.ReadGzJSON[processed](r, Name)
	if err != nil {
		return err
	}
	if len(p.Zones) < 50 {
		return fmt.Errorf("sensible: implausible zone count %d (upstream has 62)", len(p.Zones))
	}
	for _, z := range p.Zones {
		if z.Name == "" {
			return errors.New("sensible: zone with empty name")
		}
		mp := z.G.MultiPolygon()
		if len(mp) == 0 || len(mp[0]) == 0 || len(mp[0][0]) < 3 {
			return fmt.Errorf("sensible: zone %q has no usable ring", z.Name)
		}
	}
	return nil
}

// zipMember returns the decompressed content of the first member whose name
// ends with suffix (case-insensitive).
func zipMember(zr *zip.Reader, suffix string) ([]byte, error) {
	for _, f := range zr.File {
		if !strings.HasSuffix(strings.ToLower(f.Name), suffix) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("sensible: open zip member %s: %w", f.Name, err)
		}
		defer func() { _ = rc.Close() }()
		return io.ReadAll(rc)
	}
	return nil, fmt.Errorf("sensible: no %s member in raw zip", suffix)
}

// readSHPPolygons parses an ESRI shapefile holding type-5 (Polygon) records
// and returns, per record, its rings as [vertex][lon,lat]. Null shapes
// (type 0) yield an empty ring list. Only the subset of the spec the QRR file
// uses is implemented — single .shp, no .shx index needed (records are walked
// sequentially).
func readSHPPolygons(b []byte) ([][][][2]float64, error) {
	const headerLen = 100
	if len(b) < headerLen {
		return nil, errors.New("sensible: shapefile too short")
	}
	// File length lives at byte 24, big-endian, in 16-bit words.
	fileLen := int(binary.BigEndian.Uint32(b[24:28])) * 2
	if fileLen > len(b) {
		return nil, fmt.Errorf("sensible: shapefile truncated (header says %d bytes, got %d)", fileLen, len(b))
	}
	var shapes [][][][2]float64
	off := headerLen
	for off+8 <= fileLen {
		contentLen := int(binary.BigEndian.Uint32(b[off+4:off+8])) * 2
		rec := b[off+8 : off+8+contentLen]
		off += 8 + contentLen

		shapeType := int(binary.LittleEndian.Uint32(rec[0:4]))
		switch shapeType {
		case 0: // null shape
			shapes = append(shapes, nil)
			continue
		case 5: // polygon
		default:
			return nil, fmt.Errorf("sensible: unsupported shape type %d (want 5=Polygon)", shapeType)
		}
		// Polygon record: bbox (4 float64) then numParts, numPoints, the part
		// start indices, and the (x, y) point pairs.
		numParts := int(binary.LittleEndian.Uint32(rec[36:40]))
		numPoints := int(binary.LittleEndian.Uint32(rec[40:44]))
		parts := make([]int, numParts+1)
		for i := range numParts {
			parts[i] = int(binary.LittleEndian.Uint32(rec[44+4*i : 48+4*i]))
		}
		parts[numParts] = numPoints
		ptsOff := 44 + 4*numParts
		rings := make([][][2]float64, 0, numParts)
		for pi := range numParts {
			ring := make([][2]float64, 0, parts[pi+1]-parts[pi])
			for k := parts[pi]; k < parts[pi+1]; k++ {
				x := math.Float64frombits(binary.LittleEndian.Uint64(rec[ptsOff+16*k : ptsOff+16*k+8]))
				y := math.Float64frombits(binary.LittleEndian.Uint64(rec[ptsOff+16*k+8 : ptsOff+16*k+16]))
				ring = append(ring, [2]float64{x, y})
			}
			rings = append(rings, ring)
		}
		shapes = append(shapes, rings)
	}
	return shapes, nil
}

// readDBF parses a dBASE III attribute table into one map per record (field
// name → trimmed string value). Character and numeric fields only — all the
// QRR file carries. The .cpg sidecar declares UTF-8, which is also the
// assumption here.
func readDBF(b []byte) ([]map[string]string, error) {
	if len(b) < 32 {
		return nil, errors.New("sensible: dbf too short")
	}
	numRec := int(binary.LittleEndian.Uint32(b[4:8]))
	headerSize := int(binary.LittleEndian.Uint16(b[8:10]))
	recordSize := int(binary.LittleEndian.Uint16(b[10:12]))

	type fieldDesc struct {
		name string
		len  int
	}
	var fields []fieldDesc
	for off := 32; off < headerSize && b[off] != 0x0D; off += 32 {
		name := string(bytes.SplitN(b[off:off+11], []byte{0}, 2)[0])
		fields = append(fields, fieldDesc{name: name, len: int(b[off+16])})
	}

	recs := make([]map[string]string, 0, numRec)
	for i := range numRec {
		off := headerSize + i*recordSize
		if off+recordSize > len(b) {
			return nil, fmt.Errorf("sensible: dbf truncated at record %d", i)
		}
		rec := make(map[string]string, len(fields))
		p := off + 1 // skip the deletion flag
		for _, f := range fields {
			rec[f.name] = strings.TrimSpace(string(b[p : p+f.len]))
			p += f.len
		}
		recs = append(recs, rec)
	}
	return recs, nil
}
