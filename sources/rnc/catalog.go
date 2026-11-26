package rnc

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"sync"

	"github.com/bpineau/gazetteer/dataset"
)

// A legacy datadir file takes precedence over the embed until the next
// refresh writes the seekable archive. Older installations keep their data.
var legacySet = dataset.Set{Source: Name, Version: Version, Processed: dataset.File{Name: "rnc_coproprietes.json.gz"}}

// scope serves either the commune archive or, when whole is set, an index
// held entirely in memory: a legacy datadir file, or the empty index of a
// dataset available nowhere.
type scope struct {
	archive *archive
	whole   *Index
	keep    func(string) bool
	rows    int
}

func openScope(dir string, depts []string) (*scope, error) {
	origin, err := set.Resolve(dir)
	if err != nil {
		return nil, err
	}
	if origin != dataset.OriginDatadir {
		old, err := legacySet.Resolve(dir)
		if err != nil {
			return nil, err
		}
		if old == dataset.OriginDatadir {
			return openLegacy(dir, depts)
		}
	}
	if origin == dataset.OriginNone {
		// Never downloaded and not embedded: no data, not a failure.
		return &scope{whole: &Index{}}, nil
	}
	var a *archive
	if origin == dataset.OriginDatadir {
		a, err = datadirArchive(dir)
	} else {
		a, err = embeddedArchive()
	}
	if err != nil {
		return nil, err
	}
	s := &scope{archive: a, keep: deptMatcher(depts), rows: a.RowCount}
	if s.keep != nil {
		s.rows = 0
		for code, n := range a.Counts {
			if s.keep(code) {
				s.rows += n
			}
		}
	}
	return s, nil
}

// openLegacy parses a pre-archive datadir file in full. Its resident set is
// the whole scope (hundreds of MB nationally), so it warns until a refresh
// replaces it.
func openLegacy(dir string, depts []string) (*scope, error) {
	f, err := legacySet.Open(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	idx, err := parseIndexStream(f, depts)
	if err != nil {
		return nil, err
	}
	slog.Warn("rnc: legacy datadir file loaded in full; run `gazetteer refresh rnc` to switch to the commune archive",
		slog.String("file", legacySet.Processed.Name), slog.Int("rows", idx.Count()))
	return &scope{whole: idx, rows: idx.Count()}, nil
}

var embeddedArchive = sync.OnceValues(func() (*archive, error) {
	f, err := set.Open("")
	if err != nil {
		return nil, err
	}
	// embed.FS has no operating system descriptor. The ZIP reader retains
	// the immutable ReaderAt so queries only touch the member they need.
	return readArchive(f)
})

// Keep the compressed datadir snapshot after closing its file. Concurrent
// refreshes can replace it without mixing members from two vintages.
func datadirArchive(dir string) (*archive, error) {
	f, err := set.Open(dir)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	file, ok := f.(fs.File)
	if !ok {
		return nil, errors.New("RNC archive has no file size")
	}
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	data := make([]byte, info.Size())
	if _, err = io.ReadFull(f, data); err != nil {
		return nil, err
	}
	return openArchive(bytes.NewReader(data), int64(len(data)))
}

func (s *scope) commune(code string) (*Index, error) {
	if s.whole != nil {
		return s.whole, nil
	}
	if s.keep != nil && !s.keep(code) {
		return &Index{Meta: s.archive.Meta}, nil
	}
	return s.archive.commune(code)
}

var nationalScope scopeCache

type scopeCache struct {
	once sync.Once
	s    *scope
	err  error
}

func (c *scopeCache) load(dir string, depts []string) (*scope, error) {
	c.once.Do(func() { c.s, c.err = openScope(dir, depts) })
	return c.s, c.err
}
