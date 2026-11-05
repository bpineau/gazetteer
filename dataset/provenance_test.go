package dataset

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestRefresh_MovedRawURLIsDownloaded pins that a new Version moving its raw
// input to another URL under the same file name downloads it, instead of
// rebuilding from the old bytes and recording the new URL as provenance;
// a version bump on the same URL still reuses the raw file.
func TestRefresh_MovedRawURLIsDownloaded(t *testing.T) {
	t.Parallel()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, strings.TrimPrefix(r.URL.Path, "/")+"\n")
	}))
	defer srv.Close()
	dir := t.TempDir()
	c := newTestClient(t)
	set := func(version int, path string) Set {
		return Set{Source: "moved", Version: version, Processed: File{Name: "moved.csv.gz"},
			Raw: []File{{Name: "in.csv", URL: srv.URL + "/" + path}}, Transform: upperGzipTransform}
	}
	read := func(s Set) string {
		t.Helper()
		rc, err := s.Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = rc.Close() }()
		zr, err := gzip.NewReader(rc)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(zr)
		return strings.TrimSpace(string(b))
	}

	for _, step := range []struct {
		version  int
		path     string
		wantHits int32
		want     string
	}{
		{1, "v1", 1, "V1"},
		{2, "v1", 1, "V1"}, // same URL: the raw file is reused
		{3, "v2", 2, "V2"}, // moved URL: downloaded again
	} {
		if _, err := Refresh(context.Background(), c, []Set{set(step.version, step.path)}, RefreshOptions{Dir: dir}); err != nil {
			t.Fatalf("v%d: %v", step.version, err)
		}
		if got := hits.Load(); got != step.wantHits {
			t.Errorf("v%d: %d downloads, want %d", step.version, got, step.wantHits)
		}
		if got := read(set(step.version, step.path)); got != step.want {
			t.Errorf("v%d: processed %q, want %q", step.version, got, step.want)
		}
	}
}

func TestCheckNoCollisions_AllArtifacts(t *testing.T) {
	t.Parallel()
	a := Set{Source: "a", Processed: File{Name: "a.json.gz"}, Raw: []File{{Name: "shared.json", URL: "https://x/a"}}}
	cases := []struct {
		name string
		b    Set
		ok   bool
	}{
		{"same raw, other URL", Set{Source: "b", Processed: File{Name: "b.json.gz"}, Raw: []File{{Name: "shared.json", URL: "https://x/b"}}}, false},
		{"same raw, same URL", Set{Source: "b", Processed: File{Name: "b.json.gz"}, Raw: []File{{Name: "shared.json", URL: "https://x/a"}}}, true},
		{"raw named like a processed file", Set{Source: "b", Processed: File{Name: "b.json.gz"}, Raw: []File{{Name: "a.json.gz", URL: "https://x/b"}}}, false},
		{"processed named like a manifest", Set{Source: "b", Processed: File{Name: "a.manifest.json"}}, false},
	}
	for _, c := range cases {
		if err := checkNoCollisions([]Set{a, c.b}); (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok=%v", c.name, err, c.ok)
		}
	}
}

func TestReadGzJSON_Integrity(t *testing.T) {
	t.Parallel()
	var good bytes.Buffer
	if err := WriteGzJSON(&good, map[string]int{"v": 7}); err != nil {
		t.Fatal(err)
	}
	b := good.Bytes()

	corrupt := bytes.Clone(b)
	corrupt[len(corrupt)-8] ^= 0xff // flip a CRC-32 byte of the trailer
	truncated := b[:len(b)-4]       // lose the length field
	var trailing bytes.Buffer
	zw := gzip.NewWriter(&trailing)
	_, _ = io.WriteString(zw, `{"v":7} {"v":8}`)
	_ = zw.Close()

	for name, in := range map[string][]byte{"corrupt CRC": corrupt, "truncated trailer": truncated, "trailing JSON": trailing.Bytes()} {
		if v, err := ReadGzJSON[map[string]int](bytes.NewReader(in), "t"); err == nil {
			t.Errorf("%s: decoded %v with no error", name, *v)
		}
	}
	if _, err := ReadGzJSON[map[string]int](bytes.NewReader(b), "t"); err != nil {
		t.Errorf("intact artifact: %v", err)
	}
}
