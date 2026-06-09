package dataset

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"testing/fstest"
)

type gzIndex struct {
	Communes map[string]int `json:"communes"`
}

func TestGzJSONRoundTrip(t *testing.T) {
	in := gzIndex{Communes: map[string]int{"93048": 7, "75056": 3}}
	var buf bytes.Buffer
	if err := WriteGzJSON(&buf, in); err != nil {
		t.Fatalf("WriteGzJSON: %v", err)
	}
	out, err := ReadGzJSON[gzIndex](&buf, "test")
	if err != nil {
		t.Fatalf("ReadGzJSON: %v", err)
	}
	if len(out.Communes) != 2 || out.Communes["93048"] != 7 {
		t.Errorf("round-trip mismatch: %+v", out)
	}
}

func TestReadGzJSONErrors(t *testing.T) {
	if _, err := ReadGzJSON[gzIndex](strings.NewReader("not gzip"), "boom"); err == nil || !strings.Contains(err.Error(), "boom: gunzip") {
		t.Errorf("bad gzip: err = %v, want boom: gunzip", err)
	}
	var buf bytes.Buffer
	if err := WriteGzJSON(&buf, "a string, not an object"); err != nil {
		t.Fatalf("WriteGzJSON: %v", err)
	}
	if _, err := ReadGzJSON[gzIndex](&buf, "boom"); err == nil || !strings.Contains(err.Error(), "boom: parse json") {
		t.Errorf("bad json shape: err = %v, want boom: parse json", err)
	}
}

func TestLazyLoad(t *testing.T) {
	var artifact bytes.Buffer
	if err := WriteGzJSON(&artifact, gzIndex{Communes: map[string]int{"93001": 1}}); err != nil {
		t.Fatalf("WriteGzJSON: %v", err)
	}
	set := Set{
		Source:    "gzjsontest",
		Version:   1,
		Embed:     fstest.MapFS{"data/idx.json.gz": &fstest.MapFile{Data: artifact.Bytes()}},
		Processed: File{Name: "idx.json.gz"},
	}

	var lazy Lazy[gzIndex]
	parses := 0
	parse := func(r io.Reader) (*gzIndex, error) {
		parses++
		return ReadGzJSON[gzIndex](r, "gzjsontest")
	}
	for range 3 {
		idx, err := lazy.Load(set, "", parse)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if idx.Communes["93001"] != 1 {
			t.Errorf("Load returned wrong data: %+v", idx)
		}
	}
	if parses != 1 {
		t.Errorf("parse ran %d times, want 1 (singleton)", parses)
	}
}

func TestLazyLoadUnavailable(t *testing.T) {
	set := Set{
		Source:    "gzjsontest",
		Version:   1,
		Processed: File{Name: "missing.json.gz"},
	}
	var lazy Lazy[gzIndex]
	idx, err := lazy.Load(set, "", func(r io.Reader) (*gzIndex, error) {
		t.Fatal("parse must not run for an unavailable artifact")
		return nil, nil
	})
	if err != nil {
		t.Fatalf("Load: %v (unavailable must degrade gracefully)", err)
	}
	if idx == nil || len(idx.Communes) != 0 {
		t.Errorf("Load = %+v, want zero-valued index", idx)
	}
}
