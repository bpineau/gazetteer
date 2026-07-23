package dataset

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"
)

// embedFor returns a fs.FS that mimics a source's embed.FS: the processed
// artifact lives under data/<name>.
func embedFor(name, body string) fstest.MapFS {
	return fstest.MapFS{
		"data/" + name: &fstest.MapFile{Data: []byte(body)},
	}
}

func openString(t *testing.T, s Set, dir string) string {
	t.Helper()
	rc, err := s.Open(dir)
	if err != nil {
		t.Fatalf("Open(%q): %v", dir, err)
	}
	defer func() { _ = rc.Close() }()
	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	return string(b)
}

func TestOpen_EmbedOnly(t *testing.T) {
	t.Parallel()
	s := Set{Source: "foo", Version: 1, Embed: embedFor("foo.json", `{"v":"embed"}`), Processed: File{Name: "foo.json"}}
	if got := openString(t, s, ""); got != `{"v":"embed"}` {
		t.Errorf("embed body = %q", got)
	}
	// A datadir with no file still falls back to embed.
	if got := openString(t, s, t.TempDir()); got != `{"v":"embed"}` {
		t.Errorf("fallback body = %q", got)
	}
}

func TestOpen_HandPlacedDatadirWins(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "foo.json"), `{"v":"datadir"}`)
	s := Set{Source: "foo", Version: 1, Embed: embedFor("foo.json", `{"v":"embed"}`), Processed: File{Name: "foo.json"}}
	// No manifest entry → hand-placed file is trusted over the embed.
	if got := openString(t, s, dir); got != `{"v":"datadir"}` {
		t.Errorf("datadir body = %q, want hand-placed to win", got)
	}
}

func TestOpen_VersionGate(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "foo.json"), `{"v":"datadir"}`)
	// Manifest records the file at version 2.
	m := &manifest{Source: "foo"}
	m.put(manifestEntry{Name: "foo.json", SourceVersion: 2})
	if err := writeManifest(dir, m); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}
	embed := embedFor("foo.json", `{"v":"embed"}`)

	// Matching version → datadir wins.
	matched := Set{Source: "foo", Version: 2, Embed: embed, Processed: File{Name: "foo.json"}}
	if got := openString(t, matched, dir); got != `{"v":"datadir"}` {
		t.Errorf("matched version: body = %q, want datadir", got)
	}
	// Mismatched version → deterministic fall back to embed.
	stale := Set{Source: "foo", Version: 1, Embed: embed, Processed: File{Name: "foo.json"}}
	if got := openString(t, stale, dir); got != `{"v":"embed"}` {
		t.Errorf("mismatched version: body = %q, want embed fallback", got)
	}
}

func TestOpen_CorruptManifestFallsBackToEmbed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "foo.json"), `{"v":"datadir"}`)
	// A garbage manifest must not sink the dataset: Open degrades to embed.
	mustWrite(t, filepath.Join(dir, "foo.manifest.json"), `{ this is not json`)
	s := Set{Source: "foo", Version: 1, Embed: embedFor("foo.json", `{"v":"embed"}`), Processed: File{Name: "foo.json"}}
	if got := openString(t, s, dir); got != `{"v":"embed"}` {
		t.Errorf("corrupt manifest: body = %q, want embed fallback", got)
	}
	// And Resolve reports embed, not an error.
	if origin, err := s.Resolve(dir); err != nil || origin != OriginEmbed {
		t.Errorf("Resolve = (%v, %v), want (embed, nil)", origin, err)
	}
}

func TestOpen_Unavailable(t *testing.T) {
	t.Parallel()
	// No embed, nothing in datadir.
	s := Set{Source: "foo", Version: 1, Processed: File{Name: "foo.json"}}
	_, err := s.Open(t.TempDir())
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err = %v, want ErrUnavailable", err)
	}
}

func TestOpen_RejectsBadName(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", ".", "..", "a/b", "../escape", "sub/dir.json"} {
		s := Set{Source: "foo", Version: 1, Processed: File{Name: name}}
		if _, err := s.Open(t.TempDir()); err == nil {
			t.Errorf("Open with name %q: want error", name)
		}
	}
}

func TestCheck_RawInvariants(t *testing.T) {
	t.Parallel()
	// Transform set but raw URL missing → error.
	s := Set{
		Source:    "foo",
		Version:   1,
		Processed: File{Name: "foo.json"},
		Raw:       []File{{Name: "foo.raw.csv"}},
		Transform: func(_ context.Context, _ RawSet, _ io.Writer) error { return nil },
	}
	if err := s.check(); err == nil {
		t.Error("check: want error for raw with no URL under a Transform")
	}
	// Duplicate names → error.
	dup := Set{
		Source:    "foo",
		Version:   1,
		Processed: File{Name: "x.json"},
		Raw:       []File{{Name: "x.json", URL: "http://e"}},
	}
	if err := dup.check(); err == nil {
		t.Error("check: want error for raw colliding with processed name")
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestSet_Overdue(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name    string
		set     Set
		overdue bool
	}{
		{"untracked (no vintage/cadence)", Set{}, false},
		{"cadence but no vintage", Set{ExpectedCadenceMonths: 12}, false},
		{"vintage but no cadence", Set{Vintage: "2000-01"}, false},
		{"fresh annual", Set{Vintage: "2025-06", ExpectedCadenceMonths: 12}, false},   // deadline 2027-06
		{"at the edge", Set{Vintage: "2024-07", ExpectedCadenceMonths: 12}, false},    // deadline 2026-07 == now, not after
		{"stale annual", Set{Vintage: "2022-01", ExpectedCadenceMonths: 12}, true},    // deadline 2024-01
		{"stale quarterly", Set{Vintage: "2025-09", ExpectedCadenceMonths: 3}, true},  // deadline 2026-03
		{"malformed vintage", Set{Vintage: "nope", ExpectedCadenceMonths: 12}, false}, // never overdue
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.set.Overdue(now); got != c.overdue {
				t.Errorf("Overdue = %v, want %v", got, c.overdue)
			}
		})
	}
}
