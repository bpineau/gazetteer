package filosofi

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
	if err := transform(context.Background(), fixtureRawSet{"testdata/filosofi_sample.csv"}, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}
	if err := validate(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validate: %v", err)
	}
	idx, err := parseIndex(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}

	// Suppressed median (01999) and the non-commune FRANCE row are dropped.
	if idx.Count() != 3 {
		t.Fatalf("count = %d, want 3", idx.Count())
	}
	if e, _ := idx.Lookup("01001"); e.MedianEUR != 25820 || e.MinimaPct != 4.5 {
		t.Errorf("01001 = %+v, want median 25820 minima 4.5", e)
	}
	if e, _ := idx.Lookup("01002"); e.MedianEUR != 24480 || e.MinimaPct != 0 {
		t.Errorf("01002 = %+v, want median 24480 no minima", e)
	}
	// Thousands separator (narrow no-break space) must be stripped.
	if e, _ := idx.Lookup("75056"); e.MedianEUR != 30360 || e.MinimaPct != 6.2 {
		t.Errorf("75056 = %+v, want median 30360 minima 6.2", e)
	}
	if _, ok := idx.Lookup("01999"); ok {
		t.Error("01999 (suppressed median) must be dropped")
	}

	// National median = median of {24480, 25820, 30360} = 25820.
	if idx.Meta.NationalMedianEUR != 25820 {
		t.Errorf("NationalMedianEUR = %d, want 25820", idx.Meta.NationalMedianEUR)
	}
	if idx.Meta.DataYear != dataYear {
		t.Errorf("DataYear = %d, want %d", idx.Meta.DataYear, dataYear)
	}
}
