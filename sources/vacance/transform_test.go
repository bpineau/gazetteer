package vacance

import (
	"bytes"
	"context"
	"encoding/csv"
	"io"
	"os"
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
