package dvfagg

import (
	"strings"
	"testing"
)

const goodHeader = "INSEE_C;DEP;n;p25;p50;p75;n_small;p50_small"

// parseCSV must reject structurally broken input (so a bad refresh fails loud
// rather than silently shipping a corrupt embedded dataset) and degrade
// non-numeric cells to zero without dropping the row.
func TestParseCSV_Errors(t *testing.T) {
	cases := []struct {
		name    string
		csv     string
		wantErr bool
	}{
		{"empty input", "", true},
		{"header only", goodHeader + "\n", false},
		{"missing required column", "INSEE_C;DEP;n;p25;p50;p75;n_small\n75056;75;1;1;1;1;1\n", true},
		{"ragged row (short)", goodHeader + "\n75056;75;1\n", true},
		{"good row", goodHeader + "\n75056;75;10;1;2;3;4;5\n", false},
		{"non-numeric cells degrade to 0", goodHeader + "\n75056;75;x;y;z;w;a;b\n", false},
		{"blank INSEE row is skipped", goodHeader + "\n;75;10;1;2;3;4;5\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx, err := parseCSV(strings.NewReader(tc.csv))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got nil (idx=%v)", idx)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if idx == nil {
				t.Fatal("nil index with nil error")
			}
		})
	}

	// Non-numeric cells parse to zero but the row is still indexed.
	idx, err := parseCSV(strings.NewReader(goodHeader + "\n75056;75;x;y;z;w;a;b\n"))
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}
	if r, ok := idx.Lookup("75056"); !ok || r.N != 0 || r.PriceMedianEURM2 != 0 {
		t.Fatalf("non-numeric cells should degrade to 0, got %+v (ok=%v)", r, ok)
	}
	// A blank-INSEE row must not be indexed.
	idx, _ = parseCSV(strings.NewReader(goodHeader + "\n;75;10;1;2;3;4;5\n"))
	if idx.Count() != 0 {
		t.Fatalf("blank-INSEE row should be skipped, Count=%d", idx.Count())
	}
}

// FuzzParseCSV asserts the parser never panics on arbitrary bytes and that a
// nil error always yields a usable index.
func FuzzParseCSV(f *testing.F) {
	f.Add(goodHeader + "\n95268;95;431;1984;2313;2694;169;2549\n")
	f.Add("")
	f.Add(goodHeader + "\n")
	f.Add("INSEE_C\n75056\n")
	f.Fuzz(func(t *testing.T, data string) {
		idx, err := parseCSV(strings.NewReader(data))
		if err == nil {
			if idx == nil {
				t.Fatal("nil index with nil error")
			}
			_ = idx.Codes()
			_, _ = idx.Lookup("95268")
		}
	})
}

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
