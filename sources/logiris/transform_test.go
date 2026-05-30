package logiris

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"testing"
)

type bytesRawSet struct{ b []byte }

func (r bytesRawSet) Open(string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(r.b)), nil
}

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
	// Columns out of order; weighted float values; an IDF row, a non-IDF row
	// (dropped), a zero-RP row (dropped) and a 5-char aggregate (dropped).
	data := "IRIS;P21_LOG;P21_RP;P21_LOGVAC;P21_RP_LOC;P21_RP_LOCHLMV\n" +
		"930480604;1520,5;1413,2;97,0;1180,8;833,1\n" + // IDF, kept
		"750010101;1000,0;900,0;50,0;600,0;0,0\n" + // IDF Paris, kept (HLM 0)
		"130010101;500,0;450,0;20,0;300,0;100,0\n" + // Marseille → dropped
		"920010101;30,0;28,0;2,0;20,0;5,0\n" + // IDF but < minDwellings → dropped
		"940010199;0,0;0,0;0,0;0,0;0,0\n" + // zero RP/LOG → dropped
		"94001;100,0;90,0;5,0;60,0;10,0\n" // aggregate (5 chars) → dropped
	zipBlob := buildZip(t, map[string]string{
		"base-ic-logement-2021.CSV":      data,
		"meta_base-ic-logement-2021.CSV": "X;Y\n",
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
		t.Fatalf("Count = %d, want 2 (non-IDF + small + zero-RP + aggregate dropped)", idx.Count())
	}
	if idx.Meta.Scope != "Île-de-France" {
		t.Errorf("Scope = %q", idx.Meta.Scope)
	}

	got, ok := idx.Lookup("930480604")
	if !ok {
		t.Fatalf("missing 930480604")
	}
	// renter = 1180.8/1413.2 = 83.6 %; HLM = 833.1/1413.2 = 59.0 %;
	// vacancy = 97/1520.5 = 6.4 %; LOG 1520.5 → 1520 (round-half-to-even).
	want := Entry{RenterSharePct: 83.6, SocialHousingSharePct: 59, VacancyRatePct: 6.4, TotalLogements: 1520}
	if got != want {
		t.Errorf("930480604 = %+v, want %+v", got, want)
	}
	// The Paris row with HLM 0 omits the social share.
	if p, _ := idx.Lookup("750010101"); p.SocialHousingSharePct != 0 {
		t.Errorf("750010101 social share = %.1f, want 0 (HLM cell 0)", p.SocialHousingSharePct)
	}
}
