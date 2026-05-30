package filoiris

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"testing"
)

// bytesRawSet serves in-memory bytes as a dataset.RawSet for the transform.
type bytesRawSet struct{ b []byte }

func (r bytesRawSet) Open(string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.b)), nil
}

// buildZip wraps the given CSV bodies (keyed by member name) into a zip blob.
func buildZip(t *testing.T, members map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range members {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %q: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("zip write %q: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}

func TestTransform(t *testing.T) {
	t.Parallel()
	// Semicolon-delimited, comma decimals, ns/nd suppression markers, a
	// non-9-char aggregate row, and a meta_ member that must be ignored.
	data := "IRIS;DISP_TP6021;DISP_Q121;DISP_MED21;DISP_GI21\n" +
		"751010201;15,0;25000;31290;0,409\n" + // kept
		"930480604;25,0;14000;19270;0,293\n" + // kept
		"751010101;ns;ns;ns;ns\n" + // suppressed → dropped
		"751010105;nd;nd;nd;nd\n" + // unavailable → dropped
		"75101;10,0;20000;28000;0,400\n" // aggregate (5 chars) → dropped
	meta := "VARIABLE;LIBELLE\nDISP_MED21;Médiane\n"
	zipBlob := buildZip(t, map[string]string{
		"BASE_TD_FILO_IRIS_2021_DISP.csv":      data,
		"meta_BASE_TD_FILO_IRIS_2021_DISP.csv": meta,
	})

	var out bytes.Buffer
	if err := transform(context.Background(), bytesRawSet{zipBlob}, &out); err != nil {
		t.Fatalf("transform: %v", err)
	}
	if err := validate(bytes.NewReader(out.Bytes())); err != nil {
		t.Fatalf("validate: %v", err)
	}
	idx, err := parseIndex(bytes.NewReader(out.Bytes()))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}

	if idx.Count() != 2 {
		t.Fatalf("Count = %d, want 2 (ns/nd + aggregate rows dropped)", idx.Count())
	}
	want := map[string]Entry{
		"751010201": {MedianEUR: 31290, PovertyRatePct: 15, Gini: 0.409},
		"930480604": {MedianEUR: 19270, PovertyRatePct: 25, Gini: 0.293},
	}
	for code, w := range want {
		got, ok := idx.Lookup(code)
		if !ok {
			t.Errorf("missing IRIS %s", code)
			continue
		}
		if got != w {
			t.Errorf("IRIS %s = %+v, want %+v", code, got, w)
		}
	}
	// National median of {19270, 31290} (even count) = (19270+31290)/2.
	if idx.Meta.NationalMedianEUR != (19270+31290)/2 {
		t.Errorf("NationalMedianEUR = %d, want %d", idx.Meta.NationalMedianEUR, (19270+31290)/2)
	}
	if idx.Meta.DataYear != dataYear {
		t.Errorf("DataYear = %d, want %d", idx.Meta.DataYear, dataYear)
	}
}
