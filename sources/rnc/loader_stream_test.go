package rnc

import (
	"bytes"
	"context"
	"os"
	"runtime"
	"testing"

	"github.com/bpineau/gazetteer/dataset"
	"github.com/bpineau/gazetteer/gazetteer"
)

// gzIndex renders an Index to the gzipped-JSON wire format parseIndexStream
// consumes (the same format transform emits).
func gzIndex(t *testing.T, entries []Entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := dataset.WriteGzJSON(&buf, &Index{
		Meta:   Meta{Source: Name, DataVintage: "2026-01", RowCount: len(entries)},
		Copros: entries,
	}); err != nil {
		t.Fatalf("WriteGzJSON: %v", err)
	}
	return buf.Bytes()
}

// TestParseIndexStream_DeptFilter checks that a dept-filtered decode keeps only
// the matching departments (75, 93) and drops the rest (13, 69), while the
// national decode (nil filter) keeps everything.
func TestParseIndexStream_DeptFilter(t *testing.T) {
	t.Parallel()
	entries := []Entry{
		{Immatriculation: "AA1", INSEE: "75056", TypeSyndic: "Professionnel"},
		{Immatriculation: "BB2", INSEE: "93048", TypeSyndic: "Professionnel"},
		{Immatriculation: "CC3", INSEE: "13055", TypeSyndic: "Bénévole"},
		{Immatriculation: "DD4", INSEE: "69123", TypeSyndic: "Bénévole"},
	}
	raw := gzIndex(t, entries)

	nat, err := parseIndexStream(bytes.NewReader(raw), nil)
	if err != nil {
		t.Fatalf("national parse: %v", err)
	}
	if nat.Count() != 4 {
		t.Errorf("national Count = %d, want 4", nat.Count())
	}

	idf, err := parseIndexStream(bytes.NewReader(raw), []string{"75", "93"})
	if err != nil {
		t.Fatalf("filtered parse: %v", err)
	}
	if idf.Count() != 2 {
		t.Fatalf("filtered Count = %d, want 2 (only 75, 93)", idf.Count())
	}
	for _, insee := range []string{"13055", "69123"} {
		if len(idf.ByInsee[insee]) != 0 {
			t.Errorf("out-of-scope INSEE %s survived the filter", insee)
		}
	}
	for _, insee := range []string{"75056", "93048"} {
		if len(idf.ByInsee[insee]) != 1 {
			t.Errorf("in-scope INSEE %s missing from filtered index", insee)
		}
	}
}

// TestSource_OutOfScope_GracefulEmpty checks that a lookup for an address whose
// department was filtered out returns an empty result, never an error.
func TestSource_OutOfScope_GracefulEmpty(t *testing.T) {
	t.Parallel()
	// An IDF-scoped index (Paris only); the source queries it via the stub.
	idx := NewIndexForTest([]Entry{
		{Immatriculation: "AA1", INSEE: "75056", VoieNorm: "rue de rivoli"},
	})
	s := NewSource(Options{Index: idx})

	// An address in Marseille (13) — out of the loaded scope.
	res, err := s.QueryResult(context.Background(), gazetteer.Listing{INSEE: "13055", Address: "1 rue de la République, Marseille"})
	if err != nil {
		t.Fatalf("out-of-scope query errored (must be graceful empty): %v", err)
	}
	if !res.IsEmpty() {
		t.Errorf("out-of-scope query returned data (%+v), want IsEmpty", res)
	}
}

// TestMemoryFootprint_NationalVsFiltered loads the embedded national RNC
// dataset both unfiltered and IDF-filtered and logs the resident heap for
// each. It is skipped by default (parsing the 648k-row national artifact costs
// seconds and hundreds of MB); run it explicitly to measure:
//
//	RNC_MEM_TEST=1 go test ./sources/rnc -run MemoryFootprint -v
func TestMemoryFootprint_NationalVsFiltered(t *testing.T) {
	if os.Getenv("RNC_MEM_TEST") == "" {
		t.Skip("set RNC_MEM_TEST=1 to run the heap-footprint measurement")
	}
	idfDepts := []string{"75", "77", "78", "91", "92", "93", "94", "95"}

	measure := func(depts []string) (rows int, heapMB float64) {
		rc, err := set.Open("")
		if err != nil {
			t.Fatalf("open embedded set: %v", err)
		}
		defer func() { _ = rc.Close() }()
		idx, err := parseIndexStream(rc, depts)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		runtime.KeepAlive(idx)
		return idx.Count(), float64(m.HeapAlloc) / (1 << 20)
	}

	natRows, natHeap := measure(nil)
	t.Logf("national: %d rows, HeapAlloc %.0f MB", natRows, natHeap)
	idfRows, idfHeap := measure(idfDepts)
	t.Logf("IDF-filtered: %d rows, HeapAlloc %.0f MB", idfRows, idfHeap)
}
