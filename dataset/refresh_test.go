package dataset

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bpineau/gazetteer/helpers/httpx"
)

// upperGzipTransform reads the single raw "in.csv", upper-cases it, and
// writes it gzip-compressed. It exercises raw access + streaming output.
func upperGzipTransform(_ context.Context, raw RawSet, dst io.Writer) error {
	r, err := raw.Open("in.csv")
	if err != nil {
		return err
	}
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(dst)
	if _, err := zw.Write(bytes.ToUpper(b)); err != nil {
		return err
	}
	return zw.Close()
}

func newTestClient(t *testing.T) *httpx.Client {
	t.Helper()
	c, err := httpx.New(httpx.Options{})
	if err != nil {
		t.Fatalf("httpx.New: %v", err)
	}
	return c
}

func TestRefresh_DownloadTransformPersist(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "a,b,c\n")
	}))
	defer srv.Close()

	dir := t.TempDir()
	set := Set{
		Source:    "demo",
		Version:   3,
		Processed: File{Name: "demo.csv.gz"},
		Raw:       []File{{Name: "in.csv", URL: srv.URL}},
		Transform: upperGzipTransform,
	}

	rep, err := Refresh(context.Background(), newTestClient(t), []Set{set}, RefreshOptions{Dir: dir})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if len(rep) != 1 || rep[0].Err != nil || rep[0].Skipped {
		t.Fatalf("report = %+v", rep)
	}

	// Raw kept verbatim.
	gotRaw, _ := os.ReadFile(filepath.Join(dir, "in.csv"))
	if string(gotRaw) != "a,b,c\n" {
		t.Errorf("raw = %q", gotRaw)
	}
	// Processed is the gzip of the upper-cased raw.
	pf, err := os.Open(filepath.Join(dir, "demo.csv.gz"))
	if err != nil {
		t.Fatalf("open processed: %v", err)
	}
	defer func() { _ = pf.Close() }()
	zr, err := gzip.NewReader(pf)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	dec, _ := io.ReadAll(zr)
	if string(dec) != "A,B,C\n" {
		t.Errorf("processed decoded = %q, want A,B,C", dec)
	}

	// Manifest records the processed file at the Set's version.
	m, err := readManifest(dir, "demo")
	if err != nil || m == nil {
		t.Fatalf("readManifest: %v (nil=%v)", err, m == nil)
	}
	e, ok := m.entry("demo.csv.gz")
	if !ok || e.SourceVersion != 3 || e.SHA256 == "" || e.Bytes == 0 {
		t.Fatalf("manifest entry = %+v ok=%v", e, ok)
	}

	// And Open now prefers the version-matched datadir copy.
	rc, err := set.Open(dir)
	if err != nil {
		t.Fatalf("Open after refresh: %v", err)
	}
	_ = rc.Close()
}

func TestRefresh_SkipReadOnlySet(t *testing.T) {
	t.Parallel()
	set := Set{Source: "ro", Version: 1, Processed: File{Name: "ro.json"}} // no Transform
	rep, err := Refresh(context.Background(), newTestClient(t), []Set{set}, RefreshOptions{Dir: t.TempDir()})
	if err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if !rep[0].Skipped || rep[0].Reason == "" {
		t.Errorf("read-only set: report = %+v, want Skipped with reason", rep[0])
	}
}

func TestRefresh_PartialFailureDoesNotAbortBatch(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/missing" {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, "ok\n")
	}))
	defer srv.Close()
	dir := t.TempDir()

	good := Set{Source: "good", Version: 1, Processed: File{Name: "good.csv.gz"},
		Raw: []File{{Name: "in.csv", URL: srv.URL}}, Transform: upperGzipTransform}
	bad := Set{Source: "bad", Version: 1, Processed: File{Name: "bad.csv.gz"},
		Raw: []File{{Name: "in.csv", URL: srv.URL + "/missing"}}, Transform: upperGzipTransform}

	rep, err := Refresh(context.Background(), newTestClient(t), []Set{bad, good}, RefreshOptions{Dir: dir})
	if err == nil {
		t.Fatal("want joined error for the failing set")
	}
	if rep[0].Err == nil {
		t.Error("bad set should carry an error")
	}
	if rep[1].Err != nil {
		t.Errorf("good set should have succeeded, got %v", rep[1].Err)
	}
	if !Exists(filepath.Join(dir, "good.csv.gz")) {
		t.Error("good processed artifact should have been written despite bad set failing")
	}
}

func TestRefresh_ValidationRejectsBadTransform(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "data")
	}))
	defer srv.Close()
	dir := t.TempDir()

	// Declares a .gz processed name but writes plain (non-gzip) bytes →
	// the generic validator must reject it and leave nothing installed.
	set := Set{
		Source: "broken", Version: 1, Processed: File{Name: "broken.csv.gz"},
		Raw: []File{{Name: "in.csv", URL: srv.URL}},
		Transform: func(_ context.Context, _ RawSet, dst io.Writer) error {
			_, err := io.WriteString(dst, "not gzip")
			return err
		},
	}
	_, err := Refresh(context.Background(), newTestClient(t), []Set{set}, RefreshOptions{Dir: dir})
	if err == nil {
		t.Fatal("want validation error")
	}
	if Exists(filepath.Join(dir, "broken.csv.gz")) {
		t.Error("invalid processed artifact must not be installed")
	}
	if Exists(filepath.Join(dir, "broken.csv.gz.tmp")) {
		t.Error("temp file must be cleaned up on validation failure")
	}
}

func TestRefresh_ForceRedownload(t *testing.T) {
	t.Parallel()
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = io.WriteString(w, "x\n")
	}))
	defer srv.Close()
	dir := t.TempDir()
	set := Set{Source: "f", Version: 1, Processed: File{Name: "f.csv.gz"},
		Raw: []File{{Name: "in.csv", URL: srv.URL}}, Transform: upperGzipTransform}
	c := newTestClient(t)

	if _, err := Refresh(context.Background(), c, []Set{set}, RefreshOptions{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if _, err := Refresh(context.Background(), c, []Set{set}, RefreshOptions{Dir: dir}); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Errorf("hits = %d, want 1 (second run skips existing raw)", hits)
	}
	if _, err := Refresh(context.Background(), c, []Set{set}, RefreshOptions{Dir: dir, Force: true}); err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Errorf("hits = %d after Force, want 2", hits)
	}
}

func TestRefresh_EmitsEvents(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "y\n")
	}))
	defer srv.Close()
	var phases []string
	set := Set{Source: "e", Version: 1, Processed: File{Name: "e.csv.gz"},
		Raw: []File{{Name: "in.csv", URL: srv.URL}}, Transform: upperGzipTransform}
	_, err := Refresh(context.Background(), newTestClient(t), []Set{set}, RefreshOptions{
		Dir: t.TempDir(),
		Log: func(ev Event) { phases = append(phases, ev.Phase) },
	})
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(phases, ",")
	for _, want := range []string{"download", "transform", "validate", "write"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing phase %q in %q", want, joined)
		}
	}
}

func TestRefresh_NilClient(t *testing.T) {
	t.Parallel()
	if _, err := Refresh(context.Background(), nil, nil, RefreshOptions{Dir: t.TempDir()}); err == nil {
		t.Fatal("want error for nil client")
	}
}

func TestRefresh_RejectsProcessedNameCollision(t *testing.T) {
	t.Parallel()
	a := Set{Source: "a", Version: 1, Processed: File{Name: "clash.json"}}
	b := Set{Source: "b", Version: 1, Processed: File{Name: "clash.json"}}
	_, err := Refresh(context.Background(), newTestClient(t), []Set{a, b}, RefreshOptions{Dir: t.TempDir()})
	if err == nil || !strings.Contains(err.Error(), "clash.json") {
		t.Fatalf("want collision error mentioning clash.json, got %v", err)
	}
}

// Exists is a tiny local mirror of atomicfs.Exists to keep the test
// self-contained for assertions.
func Exists(p string) bool {
	_, err := os.Stat(p)
	return !errors.Is(err, os.ErrNotExist)
}
