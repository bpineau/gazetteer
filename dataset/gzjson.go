package dataset

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// ReadGzJSON decodes a gzipped JSON artifact — the standard processed
// format of the embedded commune-keyed indexes — into a freshly allocated
// T. The JSON is streamed through the decoder, so the gunzipped body is
// never materialised as one buffer (some artifacts gunzip to >100 MB).
// errPrefix prefixes error messages, conventionally the Source name.
func ReadGzJSON[T any](r io.Reader, errPrefix string) (*T, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("%s: gunzip: %w", errPrefix, err)
	}
	defer func() { _ = zr.Close() }()
	var v T
	if err := json.NewDecoder(zr).Decode(&v); err != nil {
		return nil, fmt.Errorf("%s: parse json: %w", errPrefix, err)
	}
	return &v, nil
}

// WriteGzJSON gzip-compresses the JSON encoding of v to dst — the inverse
// of ReadGzJSON, used by transforms to emit the processed artifact.
func WriteGzJSON(dst io.Writer, v any) error {
	zw := gzip.NewWriter(dst)
	if err := json.NewEncoder(zw).Encode(v); err != nil {
		return err
	}
	return zw.Close()
}

// Lazy is the once-per-process parse cache every dataset-backed source's
// Load function had hand-rolled identically: resolve the Set's processed
// artifact (datadir copy preferred, embedded fallback), parse it on first
// call, and serve the cached value forever after. A package declares one
// at the top level:
//
//	var lazyIndex dataset.Lazy[Index]
//
//	func Load(dir string) (*Index, error) {
//		return lazyIndex.Load(set, dir, parseIndex)
//	}
//
// The dir from the first call wins for the process lifetime. An artifact
// that is neither in the datadir nor embedded (ErrUnavailable) yields a
// zero-valued *T — graceful degradation, not an error: it means the
// (non-embedded) dataset was simply never downloaded.
type Lazy[T any] struct {
	once sync.Once
	v    *T
	err  error
}

// Load returns the singleton parsed value, parsing the Set's processed
// artifact on the first call. See the Lazy type doc for the contract.
func (l *Lazy[T]) Load(set Set, dir string, parse func(io.Reader) (*T, error)) (*T, error) {
	l.once.Do(func() {
		rc, err := set.Open(dir)
		if errors.Is(err, ErrUnavailable) {
			l.v = new(T)
			return
		}
		if err != nil {
			l.err = fmt.Errorf("%s: open dataset: %w", set.Source, err)
			return
		}
		defer func() { _ = rc.Close() }()
		l.v, l.err = parse(rc)
	})
	return l.v, l.err
}
