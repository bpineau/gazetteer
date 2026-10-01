package lovac

import (
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"os"
	"strings"
	"testing"
)

type fixtureRawSet struct{ path string }

func (f fixtureRawSet) Open(string) (io.ReadCloser, error) { return os.Open(f.path) }

func TestTransform_Golden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := transform(context.Background(), fixtureRawSet{"testdata/lovac_sample.csv"}, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}
	if err := validate(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validate: %v", err)
	}

	// Parse the produced CSV directly so we can assert the empty-rate rows
	// (which parseIndex would skip) are still written.
	cr := csv.NewReader(bytes.NewReader(buf.Bytes()))
	cr.Comma = ';'
	recs, err := cr.ReadAll()
	if err != nil {
		t.Fatalf("read output: %v", err)
	}
	got := map[string][2]string{}
	for _, r := range recs[1:] {
		got[r[0]] = [2]string{r[1], r[2]}
	}

	want := map[string][2]string{
		"01004": {"6.3", "3.36"},   // trailing zero trimmed (6.30→6.3)
		"70122": {"0.94", "14.38"}, // half-even on exact 14.375 → 14.38
		"99001": {"", ""},          // suppressed counts, kept (total>0)
		"99003": {"", ""},          // empty counts, kept (total>0)
	}
	if len(got) != len(want) {
		t.Fatalf("rows = %d (%v), want %d", len(got), got, len(want))
	}
	for insee, w := range want {
		if got[insee] != w {
			t.Errorf("%s = %v, want %v", insee, got[insee], w)
		}
	}
	if _, ok := got["99002"]; ok {
		t.Error("99002 (zero total) must be dropped")
	}
}

// TestTransform_NewestEdition pins the edition-tolerant column resolution on
// the 2026 file shape: several editions side by side, the "ff_" prefixed
// total, quoted cells. The newest edition (26) must win: vacant_26 over the
// land-file total of edition 25, never the 25 columns.
func TestTransform_NewestEdition(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := transform(context.Background(), fixtureRawSet{"testdata/lovac_sample_2026.csv"}, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}
	idx, err := parseIndex(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}
	e, ok := idx.Lookup("10001")
	if !ok {
		t.Fatal("10001 missing")
	}
	if e.VacancePct != 5 || e.VacanceLongPct != 2 {
		t.Errorf("10001 = %+v, want 5 %% / 2 %% (50/1000, 20/1000 from edition 26)", e)
	}
	if _, ok := idx.Lookup("10002"); ok {
		t.Error("10002 (suppressed edition-26 count) must have no headline rate")
	}
	if _, ok := idx.Lookup("10003"); ok {
		t.Error("10003 (zero edition-25 total) must be dropped, not read off edition 24")
	}
}

func TestLovacColumns(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		header  []string
		year    int
		wantErr bool
	}{
		{"2025 edition, legacy total", []string{"CODGEO_25", "pp_vacant_25", "pp_vacant_plus_2ans_25", "pp_total_24"}, 25, false},
		{"newest wins, any order", []string{"pp_vacant_24", "ff_pp_total_26", "CODGEO_27", "pp_vacant_plus_2ans_27", "pp_vacant_27", "ff_pp_total_25"}, 27, false},
		{"missing prior-year total", []string{"CODGEO_26", "pp_vacant_26", "pp_vacant_plus_2ans_26", "ff_pp_total_26"}, 0, true},
		{"no vacancy column", []string{"CODGEO_26", "LIBGEO_26"}, 0, true},
	}
	for _, tt := range tests {
		c, err := lovacColumns(tt.header)
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: err = %v, wantErr %v", tt.name, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && c.year != tt.year {
			t.Errorf("%s: year = %d, want %d", tt.name, c.year, tt.year)
		}
	}
}

// TestParseIndex_LegacyHeader keeps an artifact built from the 2025 edition
// (year-infixed rate columns) readable, e.g. a datadir copy from a previous
// refresh.
func TestParseIndex_LegacyHeader(t *testing.T) {
	t.Parallel()
	in := "INSEE_C;taux_vacance_25_pct;taux_vacance_long_25_pct\n01004;6.29;3.48\n"
	idx, err := parseIndex(strings.NewReader(in))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}
	if e, ok := idx.Lookup("01004"); !ok || e.VacancePct != 6.29 || e.VacanceLongPct != 3.48 {
		t.Errorf("01004 = %+v (ok %v), want 6.29 / 3.48", e, ok)
	}
}
