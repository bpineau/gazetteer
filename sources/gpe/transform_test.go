package gpe

import (
	"bytes"
	"context"
	"io"
	"testing"
)

type bytesRawSet struct{ b []byte }

func (r bytesRawSet) Open(string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.b)), nil
}

func TestTransform(t *testing.T) {
	t.Parallel()
	// ODS export shape: a JSON array of records. One valid, one missing line
	// (dropped), one with zero coords (dropped). Out of code order on input.
	raw := `[
		{"code":"GA26","libelle":"Nanterre La Boule","ligne":"L15","geo_point_2d":{"lon":2.2014,"lat":48.8877}},
		{"code":"GA03","libelle":"Aulnay","ligne":"L16","geo_point_2d":{"lon":2.4878,"lat":48.9518}},
		{"code":"GZ99","libelle":"No line","ligne":"","geo_point_2d":{"lon":2.3,"lat":48.8}},
		{"code":"GZ00","libelle":"No coords","ligne":"L18","geo_point_2d":{"lon":0,"lat":0}}
	]`
	var out bytes.Buffer
	if err := transform(context.Background(), bytesRawSet{[]byte(raw)}, &out); err != nil {
		t.Fatalf("transform: %v", err)
	}
	if err := validate(bytes.NewReader(out.Bytes())); err != nil {
		t.Fatalf("validate: %v", err)
	}
	idx, err := parseIndex(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}
	if idx.Count() != 2 {
		t.Fatalf("Count = %d, want 2 (no-line + no-coords dropped)", idx.Count())
	}
	// Code-sorted: GA03 before GA26.
	if idx.Stations[0].Code != "GA03" || idx.Stations[1].Code != "GA26" {
		t.Errorf("not code-sorted: %s, %s", idx.Stations[0].Code, idx.Stations[1].Code)
	}
	if idx.Stations[1].Name != "Nanterre La Boule" || idx.Stations[1].Line != "L15" {
		t.Errorf("GA26 = %+v, want Nanterre La Boule / L15", idx.Stations[1])
	}
}
