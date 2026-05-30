package oll

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"
)

// fixtureRawSet serves a single ZIP for one agglo's raw name and reports the
// others absent, so the tolerant transform yields exactly that agglo.
type fixtureRawSet struct{ name, path string }

func (f fixtureRawSet) Open(name string) (io.ReadCloser, error) {
	if name != f.name {
		return nil, os.ErrNotExist
	}
	return os.Open(f.path)
}

func TestTransform_Golden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	fx := fixtureRawSet{name: aggloSpecs[0].rawName(), path: "testdata/oll_l7502_sample.zip"}
	if err := transform(context.Background(), fx, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}
	if err := validate(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validate: %v", err)
	}
	var p processed
	if err := json.Unmarshal(buf.Bytes(), &p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(p.Agglos) != 1 {
		t.Fatalf("agglos = %d, want 1", len(p.Agglos))
	}
	a := p.Agglos[0]
	if a.Code != "L7502" || a.Year != 2024 || a.Name != "Agglomération parisienne hors Paris" {
		t.Errorf("agglo meta = %q/%d/%q", a.Code, a.Year, a.Name)
	}
	if len(a.Zones) != 2 {
		t.Errorf("zones = %d, want 2 (Saint-Denis, Boulogne)", len(a.Zones))
	}
	// Saint-Denis → zone 5.
	var sdZone string
	for _, z := range a.Zones {
		if z.INSEE == "93066" {
			sdZone = z.Zone
		}
	}
	if sdZone != "5" {
		t.Errorf("Saint-Denis zone = %q, want 5", sdZone)
	}
	// Three cells survive: zone5/p2, zone2/p1, and the zone5/p0 all-sizes
	// aggregate. The epoch-specific row, the Maison row, and the agglo
	// grand-total (blank Zone_calcul) are all dropped.
	if len(a.Rents) != 3 {
		t.Fatalf("rents = %d, want 3 (epoch/Maison/agglo-total rows must be filtered)", len(a.Rents))
	}
	cell := func(zone string, pieces int) *rentRow {
		for i := range a.Rents {
			if a.Rents[i].Zone == zone && a.Rents[i].Pieces == pieces {
				return &a.Rents[i]
			}
		}
		return nil
	}
	z5p2 := cell("5", 2)
	if z5p2 == nil {
		t.Fatalf("missing zone5/pieces2 cell")
	}
	if z5p2.MedianEURPerM2 != 18 || z5p2.N != 898 || z5p2.Q1EURPerM2 != 15.7 || z5p2.Q3EURPerM2 != 20.5 {
		t.Errorf("zone5/p2 = %+v, want median 18 / n 898 / q1 15.7 / q3 20.5", *z5p2)
	}
	z5p0 := cell("5", 0)
	if z5p0 == nil {
		t.Fatalf("missing zone5 all-sizes aggregate (pieces 0)")
	}
	if z5p0.MedianEURPerM2 != 16.5 || z5p0.N != 2900 {
		t.Errorf("zone5/p0 = %+v, want median 16.5 / n 2900", *z5p0)
	}
}

func TestParsePiecesHomogene(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in        string
		piece     int
		openEnded bool
		ok        bool
	}{
		{"Appart 1P", 1, false, true},
		{"Appart 3P", 3, false, true},
		{"Appart 4P+", 4, true, true},
		{"Ensemble 2P", 0, false, false},
		{"Maison 1-3P", 0, false, false},
		{"", 0, false, false},
	}
	for _, c := range cases {
		p, oe, ok := parsePiecesHomogene(c.in)
		if p != c.piece || oe != c.openEnded || ok != c.ok {
			t.Errorf("parsePiecesHomogene(%q) = %d/%v/%v, want %d/%v/%v", c.in, p, oe, ok, c.piece, c.openEnded, c.ok)
		}
	}
}

func TestZoneFromCalcul(t *testing.T) {
	t.Parallel()
	cases := map[string]string{"L7502.4.05": "5", "L7502.4.01": "1", "L7502.4.07": "7", "": ""}
	for in, want := range cases {
		if got := zoneFromCalcul(in); got != want {
			t.Errorf("zoneFromCalcul(%q) = %q, want %q", in, got, want)
		}
	}
}
