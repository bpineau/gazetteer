package carteloyers

import (
	"bytes"
	"context"
	"io"
	"os"
	"testing"
)

type fixtureRawSet struct{ path string }

func (f fixtureRawSet) Open(string) (io.ReadCloser, error) { return os.Open(f.path) }

func TestTransform_Golden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	tr := makeTransform("carte_loyers.raw.appartement.csv")
	if err := tr(context.Background(), fixtureRawSet{"testdata/carte_loyers_sample.csv"}, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}
	if err := validate(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validate: %v", err)
	}
	rows, err := parseCSV(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parseCSV: %v", err)
	}

	// Blank-INSEE row dropped; two communes kept, projected onto the 7-column
	// schema (lwr.IPm2/upr.IPm2 renamed, extra columns dropped, the Latin-1
	// LIBGEO column ignored).
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	r, ok := rows["95063"]
	if !ok {
		t.Fatal("95063 missing")
	}
	if r.Department != "95" || r.PredType != "commune" || r.NbObsCommune != 3620 {
		t.Errorf("95063 = %+v", r)
	}
	// Comma decimals are preserved → parsed into floats by parseCSV.
	if r.LoyerMedCC < 19.6 || r.LoyerMedCC > 19.8 {
		t.Errorf("95063 LoyerMedCC = %v, want ~19.70", r.LoyerMedCC)
	}
	if _, ok := rows[""]; ok {
		t.Error("blank-INSEE row must be dropped")
	}
}
