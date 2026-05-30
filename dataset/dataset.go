package dataset

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// embedDataDir is the directory, inside each Set.Embed filesystem, under
// which the processed artifact is found. The convention is fixed across
// all in-tree sources: a Source embeds `data/<Processed.Name>`.
const embedDataDir = "data"

// ErrUnavailable reports that a Set's processed artifact is present
// neither in the datadir nor embedded in the binary. A Source maps this
// to an empty result rather than a failure: it means the (non-embedded)
// dataset was simply never downloaded.
var ErrUnavailable = errors.New("dataset: not present in datadir and not embedded")

// File names a single artifact belonging to a Set.
//
//   - Name is a clean single path element used both as the datadir
//     basename and, for the processed artifact, the basename under
//     Embed/data.
//   - URL is the upstream location of a raw input (empty for the
//     processed artifact, which is derived locally).
//   - SHA256, when set on a raw input, pins its content: Refresh verifies
//     the download against it.
type File struct {
	Name   string
	URL    string
	SHA256 string
}

// RawSet gives a Transform read access to the downloaded raw inputs by
// their File.Name. The dataset package owns the readers' lifecycle; a
// Transform reads from them but must not close them.
type RawSet interface {
	Open(name string) (io.ReadCloser, error)
}

// Transform builds the processed artifact from the raw inputs, streaming
// the result to dst. It is invoked by Refresh after every raw input has
// been downloaded. A nil Transform marks a read-only Set: it can be read
// (Open) and shipped embedded, but Refresh cannot regenerate it.
type Transform func(ctx context.Context, raw RawSet, dst io.Writer) error

// Set is the binding, declared by one Source, between its embedded
// fallback data and the raw→processed pipeline that can refresh it. A
// Source ships one Set per logical dataset (usually exactly one) and
// exposes them through the gazetteer.DatasetProvider interface.
type Set struct {
	// Source is the owning Source's name. It namespaces the datadir
	// manifest (<Source>.manifest.json) and, for the CLI --go-embed-update
	// path, locates the in-repo embed directory (sources/<Source>/data).
	Source string

	// Version mirrors the owning Source's Version(). It is recorded in the
	// manifest on Refresh and gates datadir reuse in Open: a datadir file
	// produced by a different Version falls back to the embedded copy.
	Version int

	// Embed is the embedded fallback filesystem, rooted so that
	// "data/<Processed.Name>" resolves. Nil when the processed artifact is
	// too large to embed (download-only dataset).
	Embed fs.FS

	// Processed is the indexed artifact the Source loads at runtime.
	Processed File

	// Raw lists the upstream inputs, kept on disk alongside the processed
	// artifact for troubleshooting and reprocessing. Never embedded.
	Raw []File

	// Transform turns Raw into Processed. Nil marks a read-only Set.
	Transform Transform

	// Validate optionally checks freshly-produced processed bytes before
	// they replace the previous artifact. When nil, Refresh applies a
	// generic well-formedness check (see validateProcessed). A Source that
	// wants its real parser to gate publication supplies it here.
	Validate func(r io.Reader) error
}

// Open returns the processed artifact, preferring a validated datadir copy
// over the embedded fallback.
//
// Selection is deterministic and performs no parsing (and no hashing) at
// runtime:
//
//  1. If <dir>/<Processed.Name> exists:
//     - a manifest entry for it is present → use it iff the recorded
//     SourceVersion equals s.Version, else fall through to embed;
//     - no manifest entry (a hand-placed file) → trust and use it.
//  2. Else if Embed contains data/<Processed.Name> → use the embed.
//  3. Else → ErrUnavailable.
//
// The version gate makes a datadir file produced by a different library
// Version() (schema drift) fall back to the embedded copy deterministically,
// instead of being parsed and silently serving stale-schema data. A
// version-matched file that is genuinely corrupt surfaces loudly at the
// Source's own parse step. sha256 integrity is verified by Refresh and
// reported by the CLI's --list, not on the hot read path.
func (s Set) Open(dir string) (io.ReadCloser, error) {
	origin, err := s.Resolve(dir)
	if err != nil {
		return nil, err
	}
	switch origin {
	case OriginDatadir:
		return os.Open(filepath.Join(dir, s.Processed.Name)) //nolint:gosec // datadir-joined clean basename
	case OriginEmbed:
		return s.Embed.Open(path.Join(embedDataDir, s.Processed.Name))
	default:
		return nil, fmt.Errorf("%s: %w", s.Source, ErrUnavailable)
	}
}

// Origin identifies where Open resolves a Set's processed artifact from.
type Origin int

const (
	// OriginNone means the artifact is available neither in the datadir nor
	// embedded; Open would return ErrUnavailable.
	OriginNone Origin = iota
	// OriginDatadir means a validated datadir copy would be used.
	OriginDatadir
	// OriginEmbed means the embedded fallback would be used.
	OriginEmbed
)

func (o Origin) String() string {
	switch o {
	case OriginDatadir:
		return "datadir"
	case OriginEmbed:
		return "embed"
	default:
		return "none"
	}
}

// Resolve reports which source Open would read from for dir, without
// opening the artifact. It applies the same datadir-version gate as Open
// and is the basis for the CLI's `refresh --list`.
func (s Set) Resolve(dir string) (Origin, error) {
	if err := s.check(); err != nil {
		return OriginNone, err
	}
	if dir != "" && isFile(filepath.Join(dir, s.Processed.Name)) {
		use, err := s.datadirUsable(dir)
		if err != nil {
			return OriginNone, err
		}
		if use {
			return OriginDatadir, nil
		}
	}
	if s.Embed != nil {
		if f, err := s.Embed.Open(path.Join(embedDataDir, s.Processed.Name)); err == nil {
			_ = f.Close()
			return OriginEmbed, nil
		}
	}
	return OriginNone, nil
}

// datadirUsable reports whether the datadir processed file should be used,
// given the manifest gate described on Open.
func (s Set) datadirUsable(dir string) (bool, error) {
	m, err := readManifest(dir, s.Source)
	if err != nil {
		return false, err
	}
	entry, ok := m.entry(s.Processed.Name)
	if !ok {
		// No manifest, or the file is not tracked: a hand-placed file the
		// operator dropped in deliberately. Honour it.
		return true, nil
	}
	return entry.SourceVersion == s.Version, nil
}

// check validates the Set's invariants. It is cheap and called on every
// Open and Refresh so that a malformed Set fails fast with a clear message
// rather than producing a surprising file path.
func (s Set) check() error {
	if strings.TrimSpace(s.Source) == "" {
		return errors.New("dataset: Set.Source is empty")
	}
	if err := validName(s.Processed.Name); err != nil {
		return fmt.Errorf("dataset %q: processed %w", s.Source, err)
	}
	seen := map[string]bool{s.Processed.Name: true}
	for i, r := range s.Raw {
		if err := validName(r.Name); err != nil {
			return fmt.Errorf("dataset %q: raw[%d] %w", s.Source, i, err)
		}
		if seen[r.Name] {
			return fmt.Errorf("dataset %q: raw[%d] name %q collides with another file", s.Source, i, r.Name)
		}
		seen[r.Name] = true
		if s.Transform != nil && strings.TrimSpace(r.URL) == "" {
			return fmt.Errorf("dataset %q: raw[%d] %q has no URL", s.Source, i, r.Name)
		}
	}
	return nil
}

// validName accepts only a clean, single path element: non-empty, not "."
// or "..", and free of any path separator. This keeps every datadir file a
// direct child of the flat datadir and blocks traversal via a crafted Name.
func validName(name string) error {
	if name == "" {
		return errors.New("file name is empty")
	}
	if name == "." || name == ".." || name != filepath.Base(name) ||
		strings.ContainsRune(name, '/') || strings.ContainsRune(name, os.PathSeparator) {
		return fmt.Errorf("file name %q must be a clean single path element", name)
	}
	return nil
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular()
}
