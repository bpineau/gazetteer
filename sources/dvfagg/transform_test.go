package dvfagg

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"math"
	"os"
	"strings"
	"testing"
)

// fixture: 3 single-apt sales in 95268 (€/m² = 100000/40=2500, 90000/50=1800,
// 132000/30=4400) + one MULTI-lot mutation (must be dropped) + one Maison (dropped).
const rawFixture = `id_mutation,nature_mutation,type_local,valeur_fonciere,surface_reelle_bati,code_commune,nom_commune,code_departement
m1,Vente,Appartement,100000,40,95268,Garges,95
m2,Vente,Appartement,90000,50,95268,Garges,95
m3,Vente,Appartement,132000,30,95268,Garges,95
m4,Vente,Appartement,200000,45,95268,Garges,95
m4,Vente,Appartement,180000,42,95268,Garges,95
m5,Vente,Maison,300000,90,95268,Garges,95
`

func TestAccumulateFinalize(t *testing.T) {
	m := map[string]*acc{}
	if err := accumulate(strings.NewReader(rawFixture), m); err != nil {
		t.Fatalf("accumulate: %v", err)
	}
	rows := finalize(m)
	if len(rows) != 1 {
		t.Fatalf("want 1 commune, got %d", len(rows))
	}
	r := rows["95268"]
	if r.N != 3 {
		t.Fatalf("want N=3 (multi-lot m4 + maison m5 dropped), got %d", r.N)
	}
	// sorted €/m²: [1800, 2500, 4400] → p50=2500
	if math.Abs(r.PriceMedianEURM2-2500) > 0.5 {
		t.Fatalf("want p50=2500, got %v", r.PriceMedianEURM2)
	}
	// small band 18–55 m² includes all three (30,40,50) → p50_small=2500, n_small=3
	if r.NSmall != 3 || math.Abs(r.PriceMedianSmallEURM2-2500) > 0.5 {
		t.Fatalf("want n_small=3 p50_small=2500, got n=%d v=%v", r.NSmall, r.PriceMedianSmallEURM2)
	}
	if r.Dept != "95" {
		t.Fatalf("want dept 95, got %q", r.Dept)
	}
}

func TestPercentileLinear(t *testing.T) {
	xs := []float64{10, 20, 30, 40}
	if got := percentile(xs, 0.5); math.Abs(got-25) > 1e-9 {
		t.Fatalf("p50 want 25 got %v", got)
	}
	if got := percentile(xs, 0.25); math.Abs(got-17.5) > 1e-9 {
		t.Fatalf("p25 want 17.5 got %v", got)
	}
	if got := percentile([]float64{42}, 0.5); got != 42 {
		t.Fatalf("n=1 want 42 got %v", got)
	}
	if got := percentile(xs, 1.0); math.Abs(got-40) > 1e-9 {
		t.Fatalf("p=1.0 want 40 got %v", got)
	}
}

type fakeRaw map[string][]byte // name -> gzipped bytes

func (f fakeRaw) Open(name string) (io.ReadCloser, error) {
	b, ok := f[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func gz(s string) []byte {
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	_, _ = w.Write([]byte(s))
	_ = w.Close()
	return b.Bytes()
}

func TestTransform_WritesCSV(t *testing.T) {
	// flat name: "2024_95.csv.gz" (no slash — dataset.validName rejects slashes)
	raw := fakeRaw{"2024_95.csv.gz": gz(rawFixture)}
	var out bytes.Buffer
	if err := transformFiles(context.Background(), raw, []string{"2024_95.csv.gz"}, &out); err != nil {
		t.Fatalf("transform: %v", err)
	}
	got := out.String()
	if !strings.HasPrefix(got, "INSEE_C;DEP;n;p25;p50;p75;n_small;p50_small\n") {
		t.Fatalf("bad header: %q", got)
	}
	// p25/p50/p75 via linear interpolation on [1800,2500,4400] (n=3):
	// p25: 1800+0.5*(2500-1800)=2150, p50=2500, p75: 2500+0.5*(4400-2500)=3450
	if !strings.Contains(got, "95268;95;3;2150;2500;3450;3;2500") {
		t.Fatalf("missing aggregated row, got:\n%s", got)
	}
	if err := validate(strings.NewReader(got)); err != nil {
		t.Fatalf("validate rejected good output: %v", err)
	}
}

func TestBuildDeptsExcludesNonDVF(t *testing.T) {
	in := map[string]bool{}
	for _, d := range buildDepts() {
		in[d] = true
	}
	for _, d := range []string{"57", "67", "68", "976"} { // Alsace-Moselle + Mayotte: 404 in geo-dvf
		if in[d] {
			t.Errorf("dept %s must be excluded (not in DVF / would 404 the refresh)", d)
		}
	}
	for _, d := range []string{"75", "93", "2A", "974"} {
		if !in[d] {
			t.Errorf("dept %s must be present", d)
		}
	}
}
