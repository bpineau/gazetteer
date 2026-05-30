package delinquance

import (
	"bytes"
	"context"
	"io"
	"os"
	"reflect"
	"testing"
)

// fixtureRawSet serves a single named file from testdata, implementing
// dataset.RawSet for the transform under test.
type fixtureRawSet struct{ path string }

func (f fixtureRawSet) Open(string) (io.ReadCloser, error) { return os.Open(f.path) }

func TestTransform_Golden(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := transform(context.Background(), fixtureRawSet{"testdata/delinquance_sample.csv.gz"}, &buf); err != nil {
		t.Fatalf("transform: %v", err)
	}

	// The rebuilt bytes must gunzip, validate and parse.
	if err := validate(bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("validate: %v", err)
	}
	idx, err := parseIndex(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("parseIndex: %v", err)
	}

	want := map[string]Entry{
		// Published (diff) rows + one ndiff fallback (drug_trafficking).
		// The AFD drug-use row and the 2023-year row are dropped.
		"11111": {
			Population: 860,
			Rates: map[string]float64{
				"burglary":         7.9679,
				"drug_trafficking": 0.0,
				"sexual_violence":  1.3785,
				"vandalism":        3.5448,
			},
		},
		// All-suppressed small commune: rates come from the smoothed
		// complement_info_taux. The unknown "Fraude-typo" label is ignored.
		"22222": {
			Population: 270,
			Rates: map[string]float64{
				"burglary": 7.9679,
				"drug_use": 0.0,
				"fraud":    5.4607,
			},
		},
	}
	if idx.Count() != len(want) {
		t.Fatalf("count = %d, want %d", idx.Count(), len(want))
	}
	for insee, we := range want {
		got, ok := idx.Lookup(insee)
		if !ok {
			t.Errorf("%s: absent", insee)
			continue
		}
		if got.Population != we.Population {
			t.Errorf("%s: pop = %d, want %d", insee, got.Population, we.Population)
		}
		if !reflect.DeepEqual(got.Rates, we.Rates) {
			t.Errorf("%s: rates = %v, want %v", insee, got.Rates, we.Rates)
		}
	}

	// Meta is derived: pinned source/year/unit, count, and the sorted union
	// of the handles actually populated.
	if idx.Meta.Source != metaSource {
		t.Errorf("Source = %q, want %q", idx.Meta.Source, metaSource)
	}
	if idx.Meta.DataYear != targetYear {
		t.Errorf("DataYear = %d, want %d", idx.Meta.DataYear, targetYear)
	}
	if idx.Meta.Unit != metaUnit {
		t.Errorf("Unit = %q, want %q", idx.Meta.Unit, metaUnit)
	}
	if idx.Meta.RowCountCommunes != len(want) {
		t.Errorf("RowCountCommunes = %d, want %d", idx.Meta.RowCountCommunes, len(want))
	}
	wantInd := []string{
		"burglary", "drug_trafficking", "drug_use", "fraud",
		"sexual_violence", "vandalism",
	}
	if !reflect.DeepEqual(idx.Meta.Indicators, wantInd) {
		t.Errorf("Indicators = %v, want %v", idx.Meta.Indicators, wantInd)
	}
}

func TestPickRate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name              string
		diff, taux, compl string
		want              float64
		ok                bool
	}{
		{"published", "diff", "7,9679000", "1,0", 7.9679, true},
		{"suppressed-uses-fallback", "ndiff", "NA", "2,4043000", 2.4043, true},
		{"suppressed-na-fallback", "ndiff", "5,0", "NA", 0, false},
		{"published-na", "diff", "NA", "9,9", 0, false},
	}
	for _, c := range cases {
		got, ok := pickRate(c.diff, c.taux, c.compl)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%s: pickRate = (%v,%v), want (%v,%v)", c.name, got, ok, c.want, c.ok)
		}
	}
}
