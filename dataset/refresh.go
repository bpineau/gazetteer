package dataset

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/bpineau/gazetteer/helpers/httpx"
)

// defaultMaxRawBytes caps a single raw download (bomb guard). Dataset raws
// are routinely larger than the httpx client's generic API-response limit,
// so Refresh overrides it with a dataset-appropriate ceiling.
const defaultMaxRawBytes int64 = 1 << 30 // 1 GiB

// RefreshOptions tunes Refresh. The zero value is valid: it targets the
// DefaultDir, never re-downloads a raw already on disk, and discards
// progress events.
type RefreshOptions struct {
	// Dir is the target datadir. Empty resolves via ResolveDir (explicit >
	// $GAZETTEER_DATA_DIR > DefaultDir).
	Dir string

	// Force re-downloads every raw input even when a copy is already
	// present, and always rebuilds the processed artifact.
	Force bool

	// MaxRawBytes overrides the per-raw download ceiling. 0 uses
	// defaultMaxRawBytes.
	MaxRawBytes int64

	// Log receives structured progress events. Nil discards them.
	Log func(Event)
}

func (o RefreshOptions) emit(e Event) {
	if o.Log != nil {
		o.Log(e)
	}
}

// Event is a structured progress record emitted during Refresh. Phase is
// one of: "download", "transform", "validate", "write", "skip".
type Event struct {
	Source string
	Phase  string
	File   string
	Bytes  int64
	SHA    string
	Err    error
}

// Report is the per-set outcome of a Refresh call, one SetResult per input
// Set in the same order.
type Report []SetResult

// SetResult records what happened to one Set during Refresh.
type SetResult struct {
	Source    string
	Processed string
	Raw       []httpx.DownloadResult // one per raw input, in declaration order
	SHA256    string                 // of the processed artifact (when (re)built)
	Bytes     int64                  // of the processed artifact
	Embedded  bool                   // the owning Set ships an embedded fallback
	Skipped   bool                   // no Transform, or already current and !Force
	Reason    string                 // why it was skipped, when Skipped
	Err       error
}

// Refresh downloads each Set's raw input(s), runs its Transform to (re)build
// the processed artifact, validates it, and persists raw + processed +
// manifest into the datadir. It reuses the supplied httpx client for all
// downloads (retries, rate-limiting, atomic streaming, sha256).
//
// A single Set's failure never aborts the batch: every Set is attempted,
// its outcome is recorded in the returned Report, and the returned error is
// the errors.Join of all per-set failures (nil when every Set succeeded or
// was skipped).
func Refresh(ctx context.Context, c *httpx.Client, sets []Set, opts RefreshOptions) (Report, error) {
	if c == nil {
		return nil, errors.New("dataset: Refresh requires a non-nil httpx client")
	}
	if err := checkNoCollisions(sets); err != nil {
		return nil, err
	}
	dir, err := ResolveDir(opts.Dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // shared cache dir, not a secrets store
		return nil, fmt.Errorf("dataset: create datadir %s: %w", dir, err)
	}

	report := make(Report, 0, len(sets))
	var errs []error
	for _, s := range sets {
		res := refreshOne(ctx, c, s, dir, opts)
		report = append(report, res)
		if res.Err != nil {
			errs = append(errs, res.Err)
		}
	}
	return report, errors.Join(errs...)
}

// checkNoCollisions rejects a batch in which two Sets would write the same
// flat-datadir processed filename — they would clobber each other (and the
// per-source manifests could not even tell them apart). Names are expected
// to be globally unique (source-prefixed by convention).
func checkNoCollisions(sets []Set) error {
	seen := make(map[string]string, len(sets))
	for _, s := range sets {
		if prev, ok := seen[s.Processed.Name]; ok {
			return fmt.Errorf("dataset: processed file %q declared by both %q and %q", s.Processed.Name, prev, s.Source)
		}
		seen[s.Processed.Name] = s.Source
	}
	return nil
}

func refreshOne(ctx context.Context, c *httpx.Client, s Set, dir string, opts RefreshOptions) SetResult {
	res := SetResult{Source: s.Source, Processed: s.Processed.Name, Embedded: s.Embed != nil}
	if err := s.check(); err != nil {
		res.Err = err
		return res
	}
	if s.Transform == nil {
		res.Skipped, res.Reason = true, "read-only set (no Transform)"
		opts.emit(Event{Source: s.Source, Phase: "skip", File: s.Processed.Name, Err: errors.New(res.Reason)})
		return res
	}

	maxBytes := opts.MaxRawBytes
	if maxBytes == 0 {
		maxBytes = defaultMaxRawBytes
	}

	urls := make([]string, 0, len(s.Raw))
	for _, raw := range s.Raw {
		dest := filepath.Join(dir, raw.Name)
		opts.emit(Event{Source: s.Source, Phase: "download", File: raw.Name})
		dr, err := c.Download(ctx, raw.URL, dest, httpx.DownloadOptions{
			SkipIfExists:   !opts.Force,
			ExpectedSHA256: raw.SHA256,
			MaxBytes:       maxBytes,
		})
		if err != nil {
			res.Err = fmt.Errorf("dataset %q: download %s: %w", s.Source, raw.Name, err)
			opts.emit(Event{Source: s.Source, Phase: "download", File: raw.Name, Err: err})
			return res
		}
		res.Raw = append(res.Raw, dr)
		urls = append(urls, raw.URL)
		opts.emit(Event{Source: s.Source, Phase: "download", File: raw.Name, Bytes: dr.Bytes, SHA: dr.SHA256})
	}

	opts.emit(Event{Source: s.Source, Phase: "transform", File: s.Processed.Name})
	sum, n, err := buildProcessed(ctx, s, dir, opts)
	if err != nil {
		res.Err = err
		opts.emit(Event{Source: s.Source, Phase: "transform", File: s.Processed.Name, Err: err})
		return res
	}
	res.SHA256, res.Bytes = sum, n

	if err := commitManifest(dir, s, res, urls); err != nil {
		res.Err = err
		return res
	}
	opts.emit(Event{Source: s.Source, Phase: "write", File: s.Processed.Name, Bytes: n, SHA: sum})
	return res
}

// buildProcessed runs the Transform into a temp file (hashing as it goes),
// validates the result, then atomically renames it into place. It returns
// the processed artifact's sha256 and byte size.
func buildProcessed(ctx context.Context, s Set, dir string, opts RefreshOptions) (string, int64, error) {
	dest := filepath.Join(dir, s.Processed.Name)
	tmp := dest + ".tmp"

	sum, n, err := func() (string, int64, error) {
		f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644) //nolint:gosec // shared cache artifact
		if err != nil {
			return "", 0, fmt.Errorf("dataset %q: open temp: %w", s.Source, err)
		}
		defer func() { _ = f.Close() }()

		h := sha256.New()
		cw := &countingWriter{w: io.MultiWriter(f, h)}
		if err := s.Transform(ctx, dirRawSet{dir: dir}, cw); err != nil {
			return "", 0, fmt.Errorf("dataset %q: transform: %w", s.Source, err)
		}
		if err := f.Sync(); err != nil {
			return "", 0, fmt.Errorf("dataset %q: sync temp: %w", s.Source, err)
		}
		return hex.EncodeToString(h.Sum(nil)), cw.n, nil
	}()
	if err != nil {
		_ = os.Remove(tmp)
		return "", 0, err
	}

	if err := validate(s, tmp); err != nil {
		_ = os.Remove(tmp)
		return "", 0, fmt.Errorf("dataset %q: validate: %w", s.Source, err)
	}
	opts.emit(Event{Source: s.Source, Phase: "validate", File: s.Processed.Name})

	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", 0, fmt.Errorf("dataset %q: install processed: %w", s.Source, err)
	}
	return sum, n, nil
}

// validate runs the Set's validator (or the generic well-formedness check)
// against the freshly produced processed file at path.
func validate(s Set, path string) error {
	f, err := os.Open(path) //nolint:gosec // path is the temp file we just wrote
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if s.Validate != nil {
		return s.Validate(f)
	}
	return validateProcessed(s.Processed.Name, f)
}

// commitManifest writes the manifest last, as the refresh commit point.
func commitManifest(dir string, s Set, res SetResult, urls []string) error {
	m, err := loadOrInitManifest(dir, s.Source)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	m.put(manifestEntry{
		Name:          s.Processed.Name,
		SHA256:        res.SHA256,
		Bytes:         res.Bytes,
		SourceVersion: s.Version,
		FetchedAt:     now,
		URLs:          urls,
	})
	for i, raw := range s.Raw {
		if i >= len(res.Raw) {
			break
		}
		m.put(manifestEntry{
			Name:      raw.Name,
			SHA256:    res.Raw[i].SHA256,
			Bytes:     res.Raw[i].Bytes,
			FetchedAt: now,
			URLs:      []string{raw.URL},
		})
	}
	return writeManifest(dir, m)
}

// dirRawSet implements RawSet over the flat datadir.
type dirRawSet struct{ dir string }

func (d dirRawSet) Open(name string) (io.ReadCloser, error) {
	if err := validName(name); err != nil {
		return nil, err
	}
	return os.Open(filepath.Join(d.dir, name)) //nolint:gosec // name validated to a clean basename
}

// countingWriter counts bytes written through it.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}
