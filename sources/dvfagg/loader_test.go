package dvfagg

import (
	"strings"
	"testing"
)

func TestIndex_Codes(t *testing.T) {
	idx, _ := Load("")
	codes := idx.Codes()
	if len(codes) < 3 {
		t.Fatalf("want >=3 bootstrap codes, got %d", len(codes))
	}
	// sorted + contains a known code
	found := false
	for _, c := range codes {
		if c == "95268" {
			found = true
		}
	}
	if !found {
		t.Fatal("95268 missing from Codes()")
	}
}

func TestParseCSV_Lookup(t *testing.T) {
	const csv = "INSEE_C;DEP;n;p25;p50;p75;n_small;p50_small\n95268;95;431;1984;2313;2694;169;2549\n"
	idx, err := parseCSV(strings.NewReader(csv))
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	r, ok := idx.Lookup("95268")
	if !ok {
		t.Fatal("95268 not found")
	}
	if r.N != 431 || r.PriceMedianEURM2 != 2313 || r.PriceMedianSmallEURM2 != 2549 || r.NSmall != 169 || r.Dept != "95" {
		t.Fatalf("bad row: %+v", r)
	}
	if _, ok := idx.Lookup("00000"); ok {
		t.Fatal("unknown INSEE should miss")
	}
}

func TestLoad_Embedded(t *testing.T) {
	idx, err := Load("") // empty dir ⇒ embedded only
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := idx.Lookup("95268"); !ok {
		t.Fatal("embedded bootstrap row missing")
	}
}
