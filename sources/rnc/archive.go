package rnc

import (
	"archive/zip"
	"bytes"
	"container/list"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"reflect"
	"slices"
	"sync"
)

// Each ZIP member contains one commune in upstream order. Ordinals allow
// callers of Load to reconstruct the original national slice as well.
type archiveRow struct {
	Entry
	Ordinal int `json:"n"`
}

type manifest struct {
	Meta
	Counts map[string]int `json:"counts"`
}

func writeArchive(dst io.Writer, idx *Index) error {
	idx.buildLookups()
	zw := zip.NewWriter(dst)
	m := manifest{Meta: idx.Meta, Counts: make(map[string]int, len(idx.ByInsee))}
	for _, code := range slices.Sorted(maps.Keys(idx.ByInsee)) {
		positions := idx.ByInsee[code]
		m.Counts[code] = len(positions)
		w, err := zw.Create(code)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(w)
		for _, i := range positions {
			if err := enc.Encode(archiveRow{idx.Copros[i], i}); err != nil {
				return err
			}
		}
	}
	w, err := zw.Create("manifest.json")
	if err != nil {
		return err
	}
	if err := json.NewEncoder(w).Encode(m); err != nil {
		return err
	}
	return zw.Close()
}

type archive struct {
	manifest
	files map[string]*zip.File
	// A bounded decoded cache serves repeated visits to nearby communes.
	// mu guards the cache only: decoding runs outside it, so a cold commune
	// never stalls queries on other communes.
	mu      sync.Mutex
	recent  list.List
	cached  map[string]*list.Element
	pending map[string]*decoding
	weight  int64
}

// decoding is a commune being decoded outside the lock. Concurrent callers
// for the same commune wait on done and share its outcome.
type decoding struct {
	done chan struct{}
	idx  *Index
	err  error
}

type cachedCommune struct {
	code   string
	index  *Index
	weight int64
}

// Charge the backing array plus the full JSON size as an allowance for
// decoded strings and slices. This is a cache budget, not a process limit.
const communeCacheBudget = 64 << 20

var entrySize = int64(reflect.TypeFor[Entry]().Size())

func openArchive(r io.ReaderAt, size int64) (*archive, error) {
	z, err := zip.NewReader(r, size)
	if err != nil {
		return nil, err
	}
	a := &archive{files: make(map[string]*zip.File, len(z.File)), cached: make(map[string]*list.Element), pending: make(map[string]*decoding)}
	for _, f := range z.File {
		if a.files[f.Name] != nil {
			return nil, fmt.Errorf("duplicate RNC member %q", f.Name)
		}
		a.files[f.Name] = f
	}
	f := a.files["manifest.json"]
	if f == nil {
		return nil, errors.New("missing RNC manifest")
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rc.Close() }()
	// Reading to EOF also verifies the member CRC.
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &a.manifest); err != nil {
		return nil, err
	}
	delete(a.files, f.Name)
	total := 0
	for code, n := range a.Counts {
		// Even the shortest JSON row contains an INSEE code and an ordinal.
		if a.files[code] == nil || len(code) != 5 || n <= 0 || uint64(n) > a.files[code].UncompressedSize64/10 {
			return nil, fmt.Errorf("invalid RNC manifest entry %q", code)
		}
		total += n
	}
	if total <= 0 || total != a.RowCount || len(a.files) != len(a.Counts) {
		return nil, errors.New("inconsistent RNC manifest counts")
	}
	return a, nil
}

func (a *archive) visit(code string, visit func(archiveRow) error) error {
	rc, err := a.files[code].Open()
	if err != nil {
		return err
	}
	defer func() { _ = rc.Close() }()
	dec := json.NewDecoder(rc)
	n, previous := 0, -1
	for {
		var row archiveRow
		err := dec.Decode(&row)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("RNC commune %s: %w", code, err)
		}
		if row.INSEE != code || row.Ordinal <= previous || row.Ordinal >= a.RowCount || n >= a.Counts[code] {
			return fmt.Errorf("invalid RNC row for commune %s", code)
		}
		previous = row.Ordinal
		if err := visit(row); err != nil {
			return err
		}
		n++
	}
	if n != a.Counts[code] {
		return fmt.Errorf("wrong RNC row count for commune %s", code)
	}
	return nil
}

func (a *archive) commune(code string) (*Index, error) {
	if a.files[code] == nil {
		return &Index{Meta: a.Meta}, nil
	}
	a.mu.Lock()
	if hit := a.cached[code]; hit != nil {
		a.recent.MoveToFront(hit)
		a.mu.Unlock()
		return hit.Value.(cachedCommune).index, nil
	}
	if d := a.pending[code]; d != nil {
		a.mu.Unlock()
		<-d.done
		return d.idx, d.err
	}
	d := &decoding{done: make(chan struct{})}
	a.pending[code] = d
	a.mu.Unlock()

	d.idx, d.err = a.decode(code)

	a.mu.Lock()
	delete(a.pending, code)
	if d.err == nil {
		a.store(code, d.idx)
	}
	a.mu.Unlock()
	close(d.done)
	return d.idx, d.err
}

func (a *archive) decode(code string) (*Index, error) {
	idx := &Index{Meta: a.Meta, Copros: make([]Entry, 0, a.Counts[code])}
	err := a.visit(code, func(r archiveRow) error {
		idx.Copros = append(idx.Copros, r.Entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	idx.buildLookups()
	return idx, nil
}

// store caches a decoded commune, evicting the least recently used ones to
// stay within communeCacheBudget. A commune larger than the whole budget is
// served but not kept. The caller holds a.mu.
func (a *archive) store(code string, idx *Index) {
	weight := int64(a.files[code].UncompressedSize64) + int64(len(idx.Copros))*entrySize
	if weight > communeCacheBudget {
		return
	}
	for a.weight+weight > communeCacheBudget {
		victim := a.recent.Remove(a.recent.Back()).(cachedCommune)
		delete(a.cached, victim.code)
		a.weight -= victim.weight
	}
	a.cached[code] = a.recent.PushFront(cachedCommune{code, idx, weight})
	a.weight += weight
}

func (a *archive) all() (*Index, error) {
	idx := &Index{Meta: a.Meta, Copros: make([]Entry, a.RowCount)}
	for code := range a.files {
		err := a.visit(code, func(r archiveRow) error {
			if idx.Copros[r.Ordinal].INSEE != "" {
				return fmt.Errorf("duplicate RNC ordinal %d", r.Ordinal)
			}
			idx.Copros[r.Ordinal] = r.Entry
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	idx.buildLookups()
	return idx, nil
}

// The embedded file supports random access without copying its contents.
// Validation can also pass a temporary file or a sequential reader.
func readArchive(r io.Reader) (*archive, error) {
	if f, ok := r.(interface {
		io.ReaderAt
		Stat() (fs.FileInfo, error)
	}); ok {
		info, err := f.Stat()
		if err != nil {
			return nil, err
		}
		return openArchive(f, info.Size())
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return openArchive(bytes.NewReader(data), int64(len(data)))
}
