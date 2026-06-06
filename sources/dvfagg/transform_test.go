package dvfagg

import (
	"math"
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
}
