package zonescore

import (
	"context"
	"testing"

	"github.com/bpineau/gazetteer/gazetteer"
	"github.com/bpineau/gazetteer/sources/delinquance"
	"github.com/bpineau/gazetteer/sources/dvf"
	"github.com/bpineau/gazetteer/sources/nuisances"
	"github.com/bpineau/gazetteer/sources/oll"
)

type stubCollector struct{ byAddr map[string]gazetteer.Dossier }

func (s stubCollector) Collect(_ context.Context, l gazetteer.Listing) gazetteer.Dossier {
	return s.byAddr[l.Address]
}

// priceRent builds a dossier with a dvf price and an oll rent (the yield inputs).
func priceRent(priceEUR, rentEUR float64) gazetteer.Dossier {
	return dossier(
		okResult(dvf.Name, &dvf.Result{ValueEURPerM2Cents: new(int64(priceEUR * 100)), SampleSize: 10}),
		okResult(oll.Name, &oll.Result{ObservedMedianEURPerM2: rentEUR, SampleSize: 100, Confidence: "high"}),
	)
}

// TestCompare_Ranks ranks a high-yield candidate above a low-yield one (the
// rendement axis dominates) and computes the headline metrics.
func TestCompare_Ranks(t *testing.T) {
	t.Parallel()
	c := stubCollector{byAddr: map[string]gazetteer.Dossier{
		"cheap":     priceRent(3000, 18), // 7.2 % gross
		"expensive": priceRent(9000, 18), // 2.4 % gross
	}}
	cmp := Compare(context.Background(), c, []gazetteer.Listing{
		{Address: "expensive"}, {Address: "cheap"},
	})
	if len(cmp.Entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(cmp.Entries))
	}
	best := cmp.Entries[0]
	if best.Listing.Address != "cheap" || best.Rank != 1 {
		t.Errorf("rank 1 = %q (rank %d), want cheap", best.Listing.Address, best.Rank)
	}
	if best.YieldPct < 7 || best.YieldPct > 7.3 {
		t.Errorf("best yield = %.1f%%, want ~7.2", best.YieldPct)
	}
	if best.PriceEURPerM2 != 3000 || best.RentEURPerM2 != 18 {
		t.Errorf("best metrics = %.0f / %.0f, want 3000 / 18", best.PriceEURPerM2, best.RentEURPerM2)
	}
	if best.Score.Composite <= cmp.Entries[1].Score.Composite {
		t.Errorf("cheap composite %.1f should beat expensive %.1f (yield-first)", best.Score.Composite, cmp.Entries[1].Score.Composite)
	}
	if cmp.Entries[1].Rank != 2 {
		t.Errorf("second rank = %d, want 2", cmp.Entries[1].Rank)
	}
}

// TestCompare_YieldKnownRanksFirst pins the headline fairness rule: a candidate
// with a KNOWN (even low) yield outranks one whose yield is unknown, even when
// the latter's non-yield composite is higher.
func TestCompare_YieldKnownRanksFirst(t *testing.T) {
	t.Parallel()
	// withYield: a poor 2.4 % yield → rendement present but low composite.
	// noYield: no price/rent (rendement ABSENT) but excellent safety + livability
	// → a higher composite over the remaining axes.
	noYield := dossier(
		okResult(delinquance.Name, &delinquance.Result{Flag: delinquance.RiskLow, Population: 1000, Confidence: "high", Rates: map[string]float64{"x": 1}}),
		okResult(nuisances.Name, &nuisances.Result{NuisanceCount: 0, Tier: nuisances.TierCalme, Confidence: "high"}),
	)
	c := stubCollector{byAddr: map[string]gazetteer.Dossier{
		"withYield": priceRent(8000, 16), // 2.4 % gross, rendement present
		"noYield":   noYield,
	}}
	cmp := Compare(context.Background(), c, []gazetteer.Listing{
		{Address: "noYield"}, {Address: "withYield"},
	})
	if cmp.Entries[0].Listing.Address != "withYield" {
		t.Errorf("rank 1 = %q, want withYield (known yield outranks unknown)", cmp.Entries[0].Listing.Address)
	}
	// Sanity: the unknown-yield candidate really did have the higher raw composite.
	if cmp.Entries[1].Score.Composite <= cmp.Entries[0].Score.Composite {
		t.Errorf("expected noYield's composite (%.1f) to exceed withYield's (%.1f) — otherwise the test proves nothing",
			cmp.Entries[1].Score.Composite, cmp.Entries[0].Score.Composite)
	}
}

// TestCompare_Empty handles the no-listings case.
func TestCompare_Empty(t *testing.T) {
	t.Parallel()
	cmp := Compare(context.Background(), stubCollector{}, nil)
	if len(cmp.Entries) != 0 {
		t.Errorf("entries = %d, want 0", len(cmp.Entries))
	}
}
