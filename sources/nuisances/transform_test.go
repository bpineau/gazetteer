package nuisances

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
)

type fileRawSet struct{ path string }

func (f fileRawSet) Open(string) (io.ReadCloser, error) { return os.Open(f.path) }

func TestTransform_Golden(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if err := transform(context.Background(), fileRawSet{"testdata/nuisances_sample.csv"}, &out); err != nil {
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
	if len(p.Cells) != 3 {
		t.Fatalf("cells = %d, want 3", len(p.Cells))
	}
	// Cells are sorted by (lat, lon); 48.8566 is first.
	c := p.Cells[0]
	if c.Lat != 48.8566 || c.Lon != 2.3522 || c.Nuis != 3 || !c.PNE {
		t.Errorf("cell[0] = %+v, want 48.8566/2.3522/3/pne=true", c)
	}
}

func TestParseLatLon(t *testing.T) {
	t.Parallel()
	lat, lon, ok := parseLatLon("48.8566, 2.3522")
	if !ok || lat != 48.8566 || lon != 2.3522 {
		t.Errorf("parseLatLon = %v,%v,%v", lat, lon, ok)
	}
	if _, _, ok := parseLatLon("bad"); ok {
		t.Error("parseLatLon(bad) should fail")
	}
	if _, _, ok := parseLatLon("0, 0"); ok {
		t.Error("parseLatLon(0,0) should fail (sentinel)")
	}
}

func TestTierFor(t *testing.T) {
	t.Parallel()
	cases := map[int]string{0: TierCalme, 1: TierModere, 2: TierExpose, 3: TierTresExpose, 4: TierTresExpose}
	for in, want := range cases {
		if got := tierFor(in); got != want {
			t.Errorf("tierFor(%d) = %q, want %q", in, got, want)
		}
	}
}
