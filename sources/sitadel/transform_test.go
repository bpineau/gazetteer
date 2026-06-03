package sitadel

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
)

// rawSetStub feeds the transform a single in-memory raw file.
type rawSetStub struct {
	name string
	data []byte
}

func (s rawSetStub) Open(name string) (io.ReadCloser, error) {
	if name != s.name {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(s.data)), nil
}

func TestTransformGolden(t *testing.T) {
	csv, err := os.ReadFile("testdata/sample.csv")
	if err != nil {
		t.Fatalf("read sample: %v", err)
	}

	var buf bytes.Buffer
	if err := transform(context.Background(), rawSetStub{name: rawName, data: csv}, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}

	idx, err := parseIndex(&buf)
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}

	if idx.Meta.DataMillesime != dataMillesime {
		t.Errorf("DataMillesime = %q, want %q", idx.Meta.DataMillesime, dataMillesime)
	}

	// Paris arrondissement row (75101) must be dropped; the aggregate
	// 75056 kept. Marseille 13055 kept (no arrondissement rows upstream).
	if _, ok := idx.Lookup("75101"); ok {
		t.Errorf("75101 arrondissement row should be dropped from the artifact")
	}
	if _, ok := idx.Lookup("75056"); !ok {
		t.Errorf("75056 Paris aggregate should be present")
	}
	if _, ok := idx.Lookup("13055"); !ok {
		t.Errorf("13055 Marseille aggregate should be present")
	}

	// The all-zero commune 99999 has no non-zero authorised data → dropped.
	if _, ok := idx.Lookup("99999"); ok {
		t.Errorf("all-zero commune 99999 should be dropped")
	}

	// Low-dept zero-padded code preserved verbatim.
	if _, ok := idx.Lookup("01004"); !ok {
		t.Errorf("01004 should be present (zero-padded INSEE preserved)")
	}

	e, ok := idx.Lookup("78005")
	if !ok {
		t.Fatalf("78005 missing from artifact")
	}
	if e.YearStart != 2020 {
		t.Errorf("78005 YearStart = %d, want 2020", e.YearStart)
	}
	// Authorised "Tous Logements" 2020..2025: 10,12,20,5,3,6
	wantAuth := []int{10, 12, 20, 5, 3, 6}
	if !equalInts(e.Auth, wantAuth) {
		t.Errorf("78005 Auth = %v, want %v", e.Auth, wantAuth)
	}
	// Started "Tous Logements" 2020..2024 then 2025 BLANK (missing = -1):
	// 8,9,15,30,71,-1
	wantStarted := []int{8, 9, 15, 30, 71, missing}
	if !equalInts(e.Started, wantStarted) {
		t.Errorf("78005 Started = %v, want %v", e.Started, wantStarted)
	}
	// Collectif authorised 2020..2025: 6,8,14,4,2,5
	wantColl := []int{6, 8, 14, 4, 2, 5}
	if !equalInts(e.CollAuth, wantColl) {
		t.Errorf("78005 CollAuth = %v, want %v", e.CollAuth, wantColl)
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
