package overview

import (
	"testing"
)

func TestBuild_KnownCommune(t *testing.T) {
	t.Parallel()

	rows, err := Build(Options{Depts: []string{"95"}})
	if err != nil {
		t.Fatal(err)
	}
	var g *CommuneOverview
	for i := range rows {
		if rows[i].INSEE == "95268" {
			g = &rows[i]
		}
	}
	if g == nil {
		t.Fatal("95268 absent from Build results")
	}
	if g.PriceMedianSmallEURM2 <= 0 || g.RentMarketEURM2HC <= 0 || g.Dept != "95" {
		t.Fatalf("incomplete row: %+v", g)
	}
	t.Logf("95268: price_small=%.0f rent_hc=%.2f zonage=%s tendue=%s delinquance=%s qpv=%v",
		g.PriceMedianSmallEURM2, g.RentMarketEURM2HC, g.ZonageABC, g.ZoneTendue, g.DelinquanceLevel, g.QPV)
}

func TestBuild_NoDeptFilter(t *testing.T) {
	t.Parallel()

	rows, err := Build(Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("Build with no dept filter returned empty slice")
	}
	// All rows should have a non-empty Dept and non-zero price.
	for _, r := range rows {
		if r.Dept == "" {
			t.Errorf("row %s has empty Dept", r.INSEE)
		}
		if r.PriceMedianEURM2 <= 0 {
			t.Errorf("row %s has PriceMedianEURM2 = %.2f, want > 0", r.INSEE, r.PriceMedianEURM2)
		}
	}
}
