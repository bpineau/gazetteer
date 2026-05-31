package qpv

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"os"
	"testing"
)

// zipRawSet wraps an on-disk .zip and serves it under rawName, implementing
// dataset.RawSet for the transform under test (the upstream raw is a ZIP).
type zipRawSet struct{ path string }

func (z zipRawSet) Open(string) (io.ReadCloser, error) { return os.Open(z.path) }

// makeZip builds a tiny zip in memory containing the sample GeoJSON under a
// member name that mirrors the upstream layout (GEOJSON/...WGS84.geojson), so
// the transform's member-selection logic is exercised.
func makeZip(t *testing.T) string {
	t.Helper()
	geo, err := os.ReadFile("testdata/qpv_sample.geojson")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	// A decoy non-geojson member, then the real one.
	if w, err := zw.Create("GEOJSON/readme.txt"); err == nil {
		_, _ = w.Write([]byte("decoy"))
	}
	w, err := zw.Create("GEOJSON/QP2024_France_Hexagonale_Outre_Mer_WGS84.geojson")
	if err != nil {
		t.Fatalf("zip create: %v", err)
	}
	if _, err := w.Write(geo); err != nil {
		t.Fatalf("zip write: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	path := t.TempDir() + "/qpv_geo.zip"
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write zip: %v", err)
	}
	return path
}

// TestTransform_Golden feeds the tiny zipped GeoJSON fixture through transform
// and asserts the rebuilt gzipped index carries the right polygons, codes and
// commune membership (folding the blank-code row out).
func TestTransform_Golden(t *testing.T) {
	t.Parallel()
	zipPath := makeZip(t)

	var buf bytes.Buffer
	if err := transform(context.Background(), zipRawSet{zipPath}, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}
	if err := validate(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validate: %v", err)
	}
	idx, err := parseIndex(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}

	// Three QPV polygons (blank-code row skipped).
	if got := idx.PolygonCount(); got != 3 {
		t.Fatalf("PolygonCount = %d, want 3 (blank-code row skipped)", got)
	}

	// Point-in-polygon: inside the Paris square → QN07511M.
	res := idx.resolvePoint(48.05, 2.05)
	if res == nil || res.Code != "QN07511M" {
		t.Fatalf("resolvePoint(inside Paris) = %+v, want QN07511M", res)
	}
	if res.Label != "Goutte D'Or" {
		t.Errorf("Label = %q, want Goutte D'Or", res.Label)
	}

	// Commune membership: 75056 hosts QN07511M; the low-department code
	// "2691" must be zero-padded to "02691".
	if e, ok := idx.lookupCommune("75056"); !ok || len(e.QPVs) != 1 || e.QPVs[0].Code != "QN07511M" {
		t.Errorf("75056 commune entry = %+v ok=%t, want [QN07511M]", e, ok)
	}
	if e, ok := idx.lookupCommune("02691"); !ok || len(e.QPVs) != 1 || e.QPVs[0].Code != "QN00201M" {
		t.Errorf("02691 commune entry = %+v ok=%t, want [QN00201M] (zero-padded)", e, ok)
	}

	// Meta sanity.
	if idx.Meta.Source != metaSource {
		t.Errorf("Source = %q, want %q", idx.Meta.Source, metaSource)
	}
}
