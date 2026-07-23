package encadrement

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/helpers/geoindex"
)

// fixtureRawSet serves a single named file from testdata, implementing
// dataset.RawSet for the transform under test (the transforms each read a
// single raw input, so the requested name is ignored).
type fixtureRawSet struct{ path string }

func (f fixtureRawSet) Open(string) (io.ReadCloser, error) { return os.Open(f.path) }

// mapRawSet serves named raw bytes from memory, used by the KML-backed EPT
// barème transform (it reads one KML per grid cell, by name).
type mapRawSet map[string][]byte

func (m mapRawSet) Open(name string) (io.ReadCloser, error) {
	b, ok := m[name]
	if !ok {
		return nil, fmt.Errorf("mapRawSet: no such raw file %q", name)
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

// kmlZone is one Placemark's rates in a synthetic DRIHL barème KML.
type kmlZone struct {
	zone          int
	ref, min, max float64
}

// fakeDRIHLKML renders a DRIHL-shaped barème KML carrying one Placemark per
// entry (a zone may appear twice to exercise dedup).
func fakeDRIHLKML(zones []kmlZone) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	b.WriteString(`<kml xmlns="http://earth.google.com/kml/2.1"><Document>`)
	for _, z := range zones {
		fmt.Fprintf(&b, `<Placemark><ExtendedData>`+
			`<Data name="idZone"><value>%d</value></Data>`+
			`<Data name="ref"><value>%g</value></Data>`+
			`<Data name="refmin"><value>%g</value></Data>`+
			`<Data name="refmaj"><value>%g</value></Data>`+
			`</ExtendedData></Placemark>`, z.zone, z.ref, z.min, z.max)
	}
	b.WriteString(`</Document></kml>`)
	return []byte(b.String())
}

// buildEPTRawSet synthesises the full 64-cell KML grid for one EPT (namePrefix
// keys the files exactly as the transform requests them). Each cell carries
// zones {307, 308} plus a duplicate 307 (dedup). The (appartement, 2,
// 1946-1970, non-meuble) cell gets distinctive zone-307 rates so the field
// mapping can be asserted.
func buildEPTRawSet(namePrefix string) mapRawSet {
	m := mapRawSet{}
	for _, c := range eptKMLCombos() {
		z307 := kmlZone{307, 20, 15, 25}
		if !c.maison && !c.meuble && c.piece == 2 && c.epoque.label == "1946-1970" {
			z307 = kmlZone{307, 27.4, 19.2, 32.9}
		}
		m[namePrefix+"_"+c.kmlBasename()] = fakeDRIHLKML([]kmlZone{
			z307, {308, 21, 16, 26}, {307, 99, 99, 99}, // trailing dup 307 must be ignored
		})
	}
	return m
}

func TestTransformParis_Golden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := transformParis(context.Background(), fixtureRawSet{"testdata/paris_sample.json"}, &buf); err != nil {
		t.Fatalf("transformParis: %v", err)
	}
	if err := validateParis(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validateParis: %v", err)
	}

	var rows []parisRow
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The fixture has 3 rows for parisYear (2025) and 2 for 2024; only the
	// 2025 rows survive the year filter, in upstream order.
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (year filter must drop non-%d)", len(rows), parisYear)
	}
	for _, r := range rows {
		if r.Annee != parisYear {
			t.Errorf("row annee = %d, want %d", r.Annee, parisYear)
		}
	}
	// First row mapping: meuble_txt → bool, ref/min/max → *_eur_m2.
	got := rows[0]
	want := parisRow{
		Annee: 2025, IDZone: 5, IDQuartier: 38, NomQuartier: "Porte-Saint-Denis",
		CodeGrandQuartier: 7511038, Piece: 2, Epoque: "1946-1970", Meuble: false,
		RefEURPerM2: 27.4, MinEURPerM2: 19.2, MaxEURPerM2: 32.9,
	}
	if got != want {
		t.Errorf("row[0] = %+v, want %+v", got, want)
	}
}

// TestTransformEPTBareme_Golden drives the shared KML→barème transform over a
// synthetic full grid (64 cells × 2 zones), asserting row count, the
// zone-major ordering, the open-ended flag, dedup of a repeated zone, and the
// field mapping for one distinctive cell. Plaine Commune and Est Ensemble
// share the transform, so the EPT under test only sets the datadir name prefix.
func TestTransformEPTBareme_Golden(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		prefix    string
		transform func(context.Context, dataset.RawSet, io.Writer) error
		validate  func(io.Reader) error
	}{
		{"plaine_commune", eptRawNamePlaineCommune, transformPlaineCommune, validatePlaineCommune},
		{"est_ensemble", eptRawNameEstEnsemble, transformEstEnsemble, validateEstEnsemble},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := c.transform(context.Background(), buildEPTRawSet(c.prefix), &buf); err != nil {
				t.Fatalf("transform: %v", err)
			}
			if err := c.validate(bytes.NewReader(buf.Bytes())); err != nil {
				t.Fatalf("validate: %v", err)
			}
			var rows []eptBaremeRow
			if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
				t.Fatalf("decode: %v", err)
			}
			// 64 grid cells × 2 distinct zones; the duplicate 307 must be dropped.
			if len(rows) != 128 {
				t.Fatalf("rows = %d, want 128 (dedup of the repeated zone must hold)", len(rows))
			}
			// Zone-major order: row 0 is the lowest zone's non-maison / non-meublé
			// 1-pièce avant-1946 cell.
			want0 := eptBaremeRow{
				Zone: 307, Piece: 1, PieceOpenEnded: false, Epoque: "avant 1946",
				Meuble: false, Maison: false, RefEURPerM2: 20, MinEURPerM2: 15, MaxEURPerM2: 25,
			}
			if rows[0] != want0 {
				t.Errorf("row[0] = %+v, want %+v", rows[0], want0)
			}
			// The open-ended flag tracks pièce 4 exactly.
			for _, r := range rows {
				if (r.Piece == 4) != r.PieceOpenEnded {
					t.Errorf("piece %d open-ended=%v (open-ended must equal piece==4)", r.Piece, r.PieceOpenEnded)
				}
			}
			// Field mapping: the distinctive (2-pièces, 1946-1970, nu, appartement)
			// zone-307 cell round-trips its rates and axis labels.
			var got *eptBaremeRow
			for i := range rows {
				r := rows[i]
				if r.Zone == 307 && r.Piece == 2 && r.Epoque == "1946-1970" && !r.Meuble && !r.Maison {
					got = &r
				}
			}
			if got == nil {
				t.Fatal("distinctive cell (307/2/1946-1970/nu/appartement) not found")
			}
			if got.RefEURPerM2 != 27.4 || got.MinEURPerM2 != 19.2 || got.MaxEURPerM2 != 32.9 {
				t.Errorf("distinctive cell rates = %v/%v/%v, want 27.4/19.2/32.9", got.RefEURPerM2, got.MinEURPerM2, got.MaxEURPerM2)
			}
		})
	}
}

func TestTransformZones_Golden(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		transform    func(context.Context, dataset.RawSet, io.Writer) error
		fixture      string
		wantEPT      string
		wantZone     string
		wantINSEE    string
		inLon, inLat float64 // a point known to fall inside the first feature
	}{
		{
			name: "plaine_commune", transform: transformPlaineCommuneZones,
			fixture: "testdata/plaine_commune_zones_sample.json",
			wantEPT: ZoneSourcePlaineCommune, wantZone: "311", wantINSEE: "93066",
			inLon: 2.05, inLat: 48.95,
		},
		{
			name: "est_ensemble", transform: transformEstEnsembleZones,
			fixture: "testdata/est_ensemble_zones_sample.json",
			wantEPT: ZoneSourceEstEnsemble, wantZone: "307", wantINSEE: "93048",
			inLon: 2.42, inLat: 48.86,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := c.transform(context.Background(), fixtureRawSet{c.fixture}, &buf); err != nil {
				t.Fatalf("transform: %v", err)
			}
			if err := validateZones(bytes.NewReader(buf.Bytes())); err != nil {
				t.Fatalf("validateZones: %v", err)
			}
			var rows []zoneRow
			if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
				t.Fatalf("decode: %v", err)
			}
			r0 := rows[0]
			if r0.EPT != c.wantEPT || r0.Zone != c.wantZone || r0.INSEE != c.wantINSEE {
				t.Errorf("row[0] identity = ept %q zone %q insee %q, want %q/%q/%q",
					r0.EPT, r0.Zone, r0.INSEE, c.wantEPT, c.wantZone, c.wantINSEE)
			}
			// The transformed geometry must cover the known interior point.
			feats := make([]geoindex.Feature[zoneID], 0, len(rows))
			for _, z := range rows {
				feats = append(feats, geoindex.NewFeature(
					zoneID{ept: z.EPT, zone: z.Zone, insee: z.INSEE, commune: z.Commune},
					z.Polygons.MultiPolygon(),
				))
			}
			if _, ok := geoindex.New(feats).Resolve(c.inLat, c.inLon); !ok {
				t.Errorf("interior point (%v,%v) not covered by any transformed polygon", c.inLat, c.inLon)
			}
		})
	}
}

func TestTransformLyon_Golden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := transformLyon(context.Background(), fixtureRawSet{"testdata/lyon_sample.geojson"}, &buf); err != nil {
		t.Fatalf("transformLyon: %v", err)
	}
	if err := validateLyon(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validateLyon: %v", err)
	}

	var rows []lyonRow
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Two features × pieces {1,2,3} (open-ended dropped) × 5 époques × 2
	// meublé = 2 × 30 = 60 rows.
	if len(rows) != 60 {
		t.Fatalf("rows = %d, want 60 (open-ended bucket must be dropped)", len(rows))
	}
	// The open-ended "4 et plus" bucket (Piece would be 4) must be absent.
	for _, r := range rows {
		if r.Piece == 4 {
			t.Fatalf("found a piece=4 row; open-ended bucket should be dropped: %+v", r)
		}
	}
	// First feature is Villeurbanne IRIS 692660101 zone 4; first emitted cell
	// is piece 1 / 1946-1970 / meublé per upstream key order.
	r0 := rows[0]
	if r0.Insee != "69266" || r0.IRIS != "692660101" || r0.Zone != "4" || r0.Commune != "Villeurbanne" {
		t.Errorf("row[0] identity = %+v", r0)
	}
	if r0.Piece != 1 || r0.Epoque != "1946-1970" || !r0.Meuble {
		t.Errorf("row[0] cell = piece %d / %q / meuble %v, want 1 / 1946-1970 / true", r0.Piece, r0.Epoque, r0.Meuble)
	}
	if r0.RefEURPerM2 != 17.6 || r0.MinEURPerM2 == nil || *r0.MinEURPerM2 != 12.3 || r0.MaxEURPerM2 == nil || *r0.MaxEURPerM2 != 21.1 {
		t.Errorf("row[0] rates = ref %v min %v max %v, want 17.6/12.3/21.1", r0.RefEURPerM2, r0.MinEURPerM2, r0.MaxEURPerM2)
	}
}
