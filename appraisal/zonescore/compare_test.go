package zonescore

import (
	"context"
	"fmt"
	"sync/atomic"
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

// gateCollector counts its Collect calls and blocks until the ctx is done, so
// a test can hold every parallel slot and observe what happens to the queue.
type gateCollector struct {
	calls   atomic.Int32
	started chan struct{}
}

func (g *gateCollector) Collect(ctx context.Context, l gazetteer.Listing) gazetteer.Dossier {
	g.calls.Add(1)
	select {
	case g.started <- struct{}{}:
	default:
	}
	<-ctx.Done()
	return gazetteer.Dossier{Listing: l}
}

// A cancelled ctx must stop the fan-out instead of queueing (and collecting)
// every remaining candidate: before the ctx arm on the semaphore acquisition,
// an abandoned Compare still paid for every listing's Dossier.
func TestCompare_CancelledBeforeStart_CollectsNothing(t *testing.T) {
	t.Parallel()
	g := &gateCollector{started: make(chan struct{}, 1)}
	listings := make([]gazetteer.Listing, 20)
	for i := range listings {
		listings[i] = gazetteer.Listing{Address: fmt.Sprintf("addr-%02d", i)}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cmp := Compare(ctx, g, listings)
	if got := g.calls.Load(); got != 0 {
		t.Errorf("Collect calls = %d, want 0 on an already-cancelled ctx", got)
	}
	// The Comparison stays coherent: one ranked entry per input listing, each
	// carrying its own listing and an empty Dossier.
	if len(cmp.Entries) != len(listings) {
		t.Fatalf("entries = %d, want %d", len(cmp.Entries), len(listings))
	}
	seen := map[string]bool{}
	for i, e := range cmp.Entries {
		if e.Rank != i+1 {
			t.Errorf("entry %d rank = %d, want %d", i, e.Rank, i+1)
		}
		if e.Dossier.Listing.Address != e.Listing.Address {
			t.Errorf("entry %d dossier listing = %q, want %q", i, e.Dossier.Listing.Address, e.Listing.Address)
		}
		if len(e.Dossier.Results) != 0 {
			t.Errorf("entry %d has %d results, want none (never collected)", i, len(e.Dossier.Results))
		}
		seen[e.Listing.Address] = true
	}
	if len(seen) != len(listings) {
		t.Errorf("distinct entries = %d, want %d", len(seen), len(listings))
	}
}

// Cancelling mid-flight stops the queue too: only the candidates that already
// hold a slot are collected, not the whole backlog.
func TestCompare_CancelledMidFlight_LeavesQueueUncollected(t *testing.T) {
	t.Parallel()
	g := &gateCollector{started: make(chan struct{}, 1)}
	listings := make([]gazetteer.Listing, 3*maxParallelListings)
	for i := range listings {
		listings[i] = gazetteer.Listing{Address: fmt.Sprintf("addr-%02d", i)}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan Comparison, 1)
	go func() { done <- Compare(ctx, g, listings) }()

	<-g.started // the parallel slots are taken
	cancel()
	cmp := <-done

	// Nothing may start after the cancellation, so at most the candidates
	// holding a parallel slot were collected: the backlog must not run.
	if got := int(g.calls.Load()); got > maxParallelListings {
		t.Errorf("Collect calls = %d, want at most %d (the backlog must not run)", got, maxParallelListings)
	}
	if got := int(g.calls.Load()); got < 1 {
		t.Errorf("Collect calls = %d, want at least 1 (the test proves nothing otherwise)", got)
	}
	if len(cmp.Entries) != len(listings) {
		t.Errorf("entries = %d, want %d (every candidate keeps an entry)", len(cmp.Entries), len(listings))
	}
}
