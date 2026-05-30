package cdsr

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
)

type fixtureRawSet struct{ path string }

func (f fixtureRawSet) Open(string) (io.ReadCloser, error) { return os.Open(f.path) }

func TestTransform_Golden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := transform(context.Background(), fixtureRawSet{"testdata/cdsr_sample.json"}, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}
	if err := validate(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validate: %v", err)
	}
	var rows []Copro
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	// Row 0: Bondy, string lat/lon → float, ISO date → year.
	want0 := Copro{
		Name: "Résidence La Bruyère", Address: "211 avenue Galliéni", Commune: "BONDY",
		Lat: 48.907676, Lon: 2.491884, Lots: 176, LabelYear: 2016,
	}
	if rows[0] != want0 {
		t.Errorf("row[0] = %+v, want %+v", rows[0], want0)
	}
	// Row 1: blank copro name must fall back to the residence/address label.
	if rows[1].Name == "" {
		t.Error("row[1] name is empty; blank upstream name should fall back to address")
	}
}
