package dataset

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/bpineau/gazetteer/helpers/atomicfs"
)

// manifest is the per-source sidecar that records what Refresh wrote into
// the datadir. It is stored as <Source>.manifest.json next to the files it
// describes. Keeping one manifest per source (rather than one shared file)
// removes the read-modify-write race between concurrent refreshes and makes
// the CLI's --list a simple per-source read.
//
// The manifest is the commit point of a refresh: it is written last, after
// the raw and processed files are safely in place. A processed file whose
// recorded SourceVersion no longer matches the running library is ignored
// by Set.Open (schema-drift guard); a file with no manifest entry is
// treated as a deliberate hand-placed override.
type manifest struct {
	Source  string                   `json:"source"`
	Entries map[string]manifestEntry `json:"entries"`
}

// manifestEntry describes one file (processed or raw) the refresh produced.
type manifestEntry struct {
	Name          string    `json:"name"`
	SHA256        string    `json:"sha256"`
	Bytes         int64     `json:"bytes"`
	SourceVersion int       `json:"source_version,omitempty"`
	FetchedAt     time.Time `json:"fetched_at"`
	URLs          []string  `json:"urls,omitempty"`
}

func manifestPath(dir, source string) string {
	return filepath.Join(dir, source+".manifest.json")
}

// readManifest loads the manifest for source from dir. A missing manifest
// is not an error: it returns a nil *manifest so callers can treat "no
// manifest" and "no entry" uniformly via (*manifest).entry.
func readManifest(dir, source string) (*manifest, error) {
	b, err := os.ReadFile(manifestPath(dir, source))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("dataset %q: read manifest: %w", source, err)
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("dataset %q: parse manifest: %w", source, err)
	}
	return &m, nil
}

// entry returns the recorded entry for name. It is nil-safe: a nil receiver
// (no manifest on disk) reports "not found".
func (m *manifest) entry(name string) (manifestEntry, bool) {
	if m == nil || m.Entries == nil {
		return manifestEntry{}, false
	}
	e, ok := m.Entries[name]
	return e, ok
}

// put records or replaces an entry, preserving any other entries already in
// the manifest so a single-set refresh does not drop sibling records.
func (m *manifest) put(e manifestEntry) {
	if m.Entries == nil {
		m.Entries = map[string]manifestEntry{}
	}
	m.Entries[e.Name] = e
}

// writeManifest atomically persists m as <Source>.manifest.json in dir.
func writeManifest(dir string, m *manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("dataset %q: encode manifest: %w", m.Source, err)
	}
	if err := atomicfs.WriteFile(manifestPath(dir, m.Source), b, 0o644); err != nil {
		return fmt.Errorf("dataset %q: write manifest: %w", m.Source, err)
	}
	return nil
}

// loadOrInitManifest returns the existing manifest for source, or a freshly
// initialised empty one when none exists yet.
func loadOrInitManifest(dir, source string) (*manifest, error) {
	m, err := readManifest(dir, source)
	if err != nil {
		return nil, err
	}
	if m == nil {
		m = &manifest{Source: source}
	}
	m.Source = source
	return m, nil
}
