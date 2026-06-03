package iris

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/bpineau/gazetteer/helpers/geopoly"
)

type fileRawSet struct{ path string }

func (f fileRawSet) Open(string) (io.ReadCloser, error) { return os.Open(f.path) }

func TestTransform_Golden(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := transform(context.Background(), fileRawSet{"testdata/iris_sample.geojson"}, &out); err != nil {
		t.Fatalf("transform: %v", err)
	}
	gz, err := gzip.NewReader(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	var p processed
	if err := json.NewDecoder(gz).Decode(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(p.Iris) != 2 {
		t.Fatalf("iris = %d, want 2", len(p.Iris))
	}
	// Sorted by code: 751041401 < 930660903.
	if p.Iris[0].Code != "751041401" || p.Iris[0].Typ != "A" {
		t.Errorf("iris[0] = %+v, want 751041401/A", p.Iris[0])
	}
	if p.Iris[1].Code != "930660903" || p.Iris[1].Nom != "Test A" {
		t.Errorf("iris[1] = %+v, want 930660903/Test A", p.Iris[1])
	}
	// The transformed geometry must cover an interior point.
	mp := p.Iris[1].Polygons.MultiPolygon()
	if !mp.Covers(geopoly.Point{Lon: 2.05, Lat: 48.95}) {
		t.Error("interior point not covered by transformed polygon")
	}
}
