package sitadel

import (
	"bytes"
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

// rawSetStub feeds the transform in-memory raw files by name.
type rawSetStub map[string][]byte

func (s rawSetStub) Open(name string) (io.ReadCloser, error) {
	b, ok := s[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

// sampleMeta is the DIDO metadata matching testdata/sample.csv: the
// millésime it is labelled with and its data row count.
const sampleMeta = `{"rid":"9c90a880-4ba0-49b4-b99d-d7dd6c810dd0","millesime":"2026-09","rows":24,"temporal_coverage":{"start":"2013-01-01","end":"2025-12-31"}}`

// sampleRaw returns the raw set for testdata/sample.csv with the given
// metadata.
func sampleRaw(t *testing.T, meta string) rawSetStub {
	t.Helper()
	csv, err := os.ReadFile("testdata/sample.csv")
	if err != nil {
		t.Fatalf("read sample: %v", err)
	}
	return rawSetStub{rawName: csv, rawMetaName: []byte(meta)}
}

func TestTransformGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := transform(context.Background(), sampleRaw(t, sampleMeta), &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}

	idx, err := parseIndex(&buf)
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}

	// The millésime is the one the DIDO metadata names.
	if idx.Meta.DataMillesime != "2026-09" {
		t.Errorf("DataMillesime = %q, want 2026-09", idx.Meta.DataMillesime)
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

	e, ok := idx.Lookup("93066")
	if !ok {
		t.Fatalf("93066 missing from artifact")
	}
	if e.YearStart != 2020 {
		t.Errorf("93066 YearStart = %d, want 2020", e.YearStart)
	}
	// Authorised "Tous Logements" 2020..2025: 10,12,20,5,3,6
	wantAuth := []int{10, 12, 20, 5, 3, 6}
	if !equalInts(e.Auth, wantAuth) {
		t.Errorf("93066 Auth = %v, want %v", e.Auth, wantAuth)
	}
	// Started "Tous Logements" 2020..2024 then 2025 BLANK (missing = -1):
	// 8,9,15,30,71,-1
	wantStarted := []int{8, 9, 15, 30, 71, missing}
	if !equalInts(e.Started, wantStarted) {
		t.Errorf("93066 Started = %v, want %v", e.Started, wantStarted)
	}
	// Collectif authorised 2020..2025: 6,8,14,4,2,5
	wantColl := []int{6, 8, 14, 4, 2, 5}
	if !equalInts(e.CollAuth, wantColl) {
		t.Errorf("93066 CollAuth = %v, want %v", e.CollAuth, wantColl)
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

// TestTransformRejectsMillesimeMismatch pins the consistency check between
// the two latest-millésime downloads: a CSV whose row count differs from the
// metadata's belongs to another millésime and must not be labelled with it.
// Metadata without a millésime is refused too.
func TestTransformRejectsMillesimeMismatch(t *testing.T) {
	for name, meta := range map[string]string{
		"row count of another millésime": `{"millesime":"2026-12","rows":25}`,
		"no millésime":                   `{"rows":24}`,
		"not JSON":                       `<html>maintenance</html>`,
	} {
		var buf bytes.Buffer
		if err := transform(context.Background(), sampleRaw(t, meta), &buf); err == nil {
			t.Errorf("%s: transform must fail", name)
		}
	}
}

// TestRawURLsServeTheLatestMillesime keeps the millésime out of the raw URLs:
// DIDO stops serving a superseded millésime, so a pinned one breaks the
// refresh at the next publication.
func TestRawURLsServeTheLatestMillesime(t *testing.T) {
	for _, f := range set.Raw {
		if strings.Contains(f.URL, "millesime=") {
			t.Errorf("%s: URL %q pins a millésime", f.Name, f.URL)
		}
	}
}
