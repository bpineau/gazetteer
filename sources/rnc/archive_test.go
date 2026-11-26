package rnc

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func archiveFixture(t *testing.T) ([]byte, *Index) {
	t.Helper()
	idx := NewIndexForTest([]Entry{
		{INSEE: "93001", Immatriculation: "B", VoiesComp: []string{"bois"}, Parcelles: []string{"parcel"}},
		{INSEE: "31001", Immatriculation: "C"},
		{INSEE: "93001", Immatriculation: "A"},
	})
	idx.Meta = Meta{Source: Name, DataVintage: "2026-01", RowCount: idx.Count()}
	var buf bytes.Buffer
	if err := writeArchive(&buf, idx); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes(), idx
}

func TestArchivePreservesOrderAndScope(t *testing.T) {
	data, idx := archiveFixture(t)
	dir := t.TempDir()
	legacy := gzIndex(t, []Entry{{INSEE: "31001", Immatriculation: "legacy"}})
	if err := os.WriteFile(filepath.Join(dir, legacySet.Processed.Name), legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := openScope(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.legacy == nil || s.legacy.Copros[0].Immatriculation != "legacy" {
		t.Fatal("legacy datadir override was ignored")
	}
	if err := os.WriteFile(filepath.Join(dir, set.Processed.Name), data, 0o600); err != nil {
		t.Fatal(err)
	}
	s, err = openScope(dir, []string{"93"})
	if err != nil {
		t.Fatal(err)
	}
	if s.rows != 2 {
		t.Fatalf("scoped count %d, want 2", s.rows)
	}
	for code, n := range map[string]int{"93001": 2, "31001": 0, "93002": 0} {
		i, err := s.commune(code)
		if err != nil {
			t.Fatal(err)
		}
		if i.Count() != n || i.Meta != idx.Meta {
			t.Errorf("wrong count or provenance for %s", code)
		}
	}
	group, err := s.commune("93001")
	if err != nil {
		t.Fatal(err)
	}
	if group.Copros[0].Immatriculation != "B" || group.Copros[1].Immatriculation != "A" {
		t.Fatal("candidate tie order changed")
	}
	full, err := s.archive.all()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(full, idx) {
		t.Fatal("full index differs from input")
	}
}

func TestDamagedCommuneIsIsolated(t *testing.T) {
	data, _ := archiveFixture(t)
	a, err := openArchive(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	off, err := a.files["93001"].DataOffset()
	if err != nil {
		t.Fatal(err)
	}
	data[off] ^= 0xff
	if _, err = a.commune("93001"); err == nil {
		t.Fatal("damaged member yielded a result")
	}
	good, err := a.commune("31001")
	if err != nil || good.Count() != 1 {
		t.Fatalf("independent commune: %v", err)
	}
	if err = validate(bytes.NewReader(data)); err == nil {
		t.Fatal("refresh accepted a damaged archive")
	}
}
