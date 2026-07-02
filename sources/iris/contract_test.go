package iris

import (
	"bytes"
	"compress/gzip"
	"context"
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/helpers/safejson"
)

// TestSourceContract pins the uniform per-source surface: Name /
// Version / Datasets / the typed QueryResult path.
func TestSourceContract(t *testing.T) {
	t.Parallel()
	s := NewSource(Options{})
	if s.Name() != Name {
		t.Errorf("Name() = %q, want %q", s.Name(), Name)
	}
	if s.Version() < 1 {
		t.Errorf("Version() = %d, want >= 1", s.Version())
	}
	if ds := s.Datasets(); len(ds) != 1 {
		t.Errorf("Datasets() = %d sets, want 1", len(ds))
	}

	lat, lon := 48.9355, 2.3590 // Basilique de Saint-Denis
	r, err := s.QueryResult(context.Background(), gazetteer.Listing{Lat: &lat, Lon: &lon})
	if err != nil {
		t.Fatalf("QueryResult: %v", err)
	}
	if r.IsEmpty() || r.CodeIRIS == "" {
		t.Errorf("Saint-Denis must resolve to an IRIS, got %+v", r)
	}
}

// gzJSON compresses a JSON-marshalable value the way a built artifact
// is stored.
func gzJSON(t *testing.T, v any) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write(safejson.MustMarshal(v)); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestValidate(t *testing.T) {
	t.Parallel()

	// Not gzip at all.
	if err := validate(strings.NewReader("plain")); err == nil {
		t.Error("plain text must fail validation")
	}

	// Gzip of broken JSON.
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	_, _ = gz.Write([]byte("{broken"))
	_ = gz.Close()
	if err := validate(&buf); err == nil {
		t.Error("broken JSON must fail validation")
	}

	// Parses, but implausibly small.
	small := processed{Iris: []irisRow{{Code: "751010101", Polygons: [][][][2]float64{{{{2, 48}, {3, 48}, {3, 49}}}}}}}
	if err := validate(bytes.NewReader(gzJSON(t, small))); err == nil || !strings.Contains(err.Error(), "4000") {
		t.Errorf("small artifact must fail the plausibility gate, got %v", err)
	}

	// A row without geometry must fail even at plausible cardinality.
	rows := make([]irisRow, 4001)
	for i := range rows {
		rows[i] = irisRow{Code: "751010101", Polygons: [][][][2]float64{{{{2, 48}, {3, 48}, {3, 49}}}}}
	}
	rows[7] = irisRow{Code: "751010102"} // no polygons
	if err := validate(bytes.NewReader(gzJSON(t, processed{Iris: rows}))); err == nil {
		t.Error("a geometry-less row must fail validation")
	}

	// A fully plausible artifact passes.
	rows[7] = rows[0]
	if err := validate(bytes.NewReader(gzJSON(t, processed{Iris: rows}))); err != nil {
		t.Errorf("plausible artifact must validate, got %v", err)
	}
}
